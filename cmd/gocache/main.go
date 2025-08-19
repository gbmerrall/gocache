package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gbmerrall/gocache/internal/cache"
	"github.com/gbmerrall/gocache/internal/config"
	"github.com/gbmerrall/gocache/internal/control"
	"github.com/gbmerrall/gocache/internal/pidfile"
	"github.com/gbmerrall/gocache/internal/proxy"
	"github.com/gbmerrall/gocache/internal/cli"
)

var exit = os.Exit

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		exit(1)
	}
}

func run(args []string) error {
	configPath := flag.String("config", "", "Path to config file")
	daemon := flag.Bool("daemon", false, "Run as a background daemon")
	logLevel := flag.String("log-level", "", "Log level (debug, info, warn, error)")
	flag.Parse()

	if len(flag.Args()) > 0 {
		cfg, err := config.LoadConfig(*configPath)
		if err != nil {
			return fmt.Errorf("error loading config for CLI: %w", err)
		}
		return cli.Run(cfg.Server.ControlPort, flag.Args())
	}

	if *daemon {
		if _, err := pidfile.Read(); err == nil {
			return fmt.Errorf("gocache is already running")
		}
		args := os.Args[1:]
		for i, arg := range args {
			if arg == "--daemon" {
				args = append(args[:i], args[i+1:]...)
				break
			}
		}
		cmd := exec.Command(os.Args[0], args...)
		cmd.SysProcAttr = getProcAttr()
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start daemon: %w", err)
		}
		fmt.Printf("gocache started in background with PID: %d\n", cmd.Process.Pid)
		return nil
	}

	startServer(*configPath, *logLevel)
	return nil
}


func startServer(configPath, logLevelOverride string, testShutdown ...func()) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		slog.Default().Error("failed to load config", "error", err)
		exit(1)
	}

	// Get effective application logging settings
	appLevel := cfg.Logging.GetEffectiveAppLevel()
	appLogfile := cfg.Logging.GetEffectiveAppLogfile()
	
	// Override with command line if provided
	if logLevelOverride != "" {
		appLevel = logLevelOverride
	}
	
	// If application logging is disabled, create a no-op logger
	var logger *slog.Logger
	if appLevel == "" {
		// Create a logger that discards all output (application logging disabled)
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1}))
	} else {
		// Application logging is enabled, set up the logger properly
		var logWriter io.Writer = os.Stdout
		if appLogfile != "" {
			file, err := os.OpenFile(appLogfile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err != nil {
				slog.Default().Error("failed to open application log file", "error", err)
				exit(1)
			}
			logWriter = io.MultiWriter(os.Stdout, file)
		}

		var level slog.Level
		switch strings.ToLower(appLevel) {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		default:
			level = slog.LevelInfo
		}
		logger = slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: level}))
	}

	if err := pidfile.Write(); err != nil {
		logger.Error("failed to write pidfile", "error", err)
		exit(1)
	}
	defer pidfile.Remove()

	c := cache.NewMemoryCache(cfg.Cache.GetDefaultTTL())
	logger.Debug("memory cache created", "defaultTTL", cfg.Cache.GetDefaultTTL())
	if cfg.Persistence.Enable {
		logger.Debug("persistence enabled, loading cache from file", "file", cfg.Persistence.CacheFile)
		if err := c.LoadFromFile(cfg.Persistence.CacheFile); err != nil && !os.IsNotExist(err) {
			logger.Warn("failed to load cache from file", "error", err)
		} else if err == nil {
			logger.Debug("cache loaded successfully from file", "file", cfg.Persistence.CacheFile)
		}
	} else {
		logger.Debug("persistence disabled")
	}

	logger.Debug("creating proxy server", "cacheableTypes", cfg.Cache.CacheableTypes, "ignoreNoCache", cfg.Cache.IgnoreNoCache, "negativeTTL", cfg.Cache.GetNegativeTTL())
	p, err := proxy.NewProxy(logger, c, cfg)
	if err != nil {
		logger.Error("failed to create proxy", "error", err)
		exit(1)
	}
	logger.Debug("proxy server created successfully")

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := p.Shutdown(ctx); err != nil {
			logger.Error("proxy shutdown failed", "error", err)
		}
		if cfg.Persistence.Enable {
			if err := c.SaveToFile(cfg.Persistence.CacheFile); err != nil {
				logger.Error("failed to save cache to file", "error", err)
			}
		}
		if len(testShutdown) > 0 {
			testShutdown[0]()
		} else {
			exit(0)
		}
	}

	logger.Debug("creating control API", "bindAddress", cfg.Server.BindAddress, "controlPort", cfg.Server.ControlPort)
	controlAPI := control.NewControlAPI(logger, cfg, c, p, shutdown)
	go func() {
		if err := controlAPI.Start(); err != nil && err != http.ErrServerClosed {
			logger.Error("control API failed", "error", err)
			exit(1)
		}
	}()

	handleSignals(logger, shutdown, controlAPI.ReloadConfig)

	addr := fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.ProxyPort)
	logger.Info("GoCache starting", "address", addr)
	logger.Debug("server configuration", "proxyPort", cfg.Server.ProxyPort, "controlPort", cfg.Server.ControlPort, "appLevel", appLevel)

	if err := p.Start(addr); err != nil && err != http.ErrServerClosed {
		logger.Error("proxy failed", "error", err)
		exit(1)
	}
}

func handleSignals(logger *slog.Logger, shutdown func(), reload func() error) {
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		for {
			sig := <-sigchan
			switch sig {
			case syscall.SIGINT, syscall.SIGTERM:
				logger.Info("shutdown signal received, starting graceful shutdown")
				shutdown()
			case syscall.SIGHUP:
				logger.Info("SIGHUP signal received, reloading configuration")
				if err := reload(); err != nil {
					logger.Error("failed to reload configuration", "error", err)
				}
			}
		}
	}()
}
