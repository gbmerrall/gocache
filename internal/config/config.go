package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	MaxPostCacheBodySizeMB = 50
)

type Config struct {
	Server      ServerConfig      `toml:"server"`
	Cache       CacheConfig       `toml:"cache"`
	Logging     LoggingConfig     `toml:"logging"`
	Persistence PersistenceConfig `toml:"persistence"`
	LoadedPath  string            `toml:"-"` // To be populated after loading
}

type ServerConfig struct {
	ProxyPort           int    `toml:"proxy_port"`
	ControlPort         int    `toml:"control_port"`
	BindAddress         string `toml:"bind_address"`
	MaxCertCacheEntries int    `toml:"max_cert_cache_entries"`
}

type PostCacheConfig struct {
	Enable                bool `toml:"enable"`
	IncludeQueryString    bool `toml:"include_query_string"`
	MaxRequestBodySizeMB  int  `toml:"max_request_body_size_mb"`
	MaxResponseBodySizeMB int  `toml:"max_response_body_size_mb"`
}

type CacheConfig struct {
	DefaultTTL     string          `toml:"default_ttl"`
	NegativeTTL    string          `toml:"negative_ttl"`
	MaxSizeMB      int             `toml:"max_size_mb"`
	IgnoreNoCache  bool            `toml:"ignore_no_cache"`
	CacheableTypes []string        `toml:"cacheable_types"`
	PostCache      PostCacheConfig `toml:"post_cache"`
}

type LoggingConfig struct {
	// Application logs (legacy fields kept for compatibility)
	Level string `toml:"level"` // Deprecated: use AppLevel
	File  string `toml:"file"`  // Deprecated: use AppLogfile

	// Application logs (new fields)
	AppLevel   string `toml:"app_level"`
	AppLogfile string `toml:"app_logfile"`

	// Access logs
	AccessToStdout bool   `toml:"access_to_stdout"`
	AccessLogfile  string `toml:"access_logfile"`
	AccessFormat   string `toml:"access_format"`
}

type PersistenceConfig struct {
	Enable           bool   `toml:"enable"`
	CacheFile        string `toml:"cache_file"`
	AutoSaveInterval string `toml:"auto_save_interval"`
}

func (c *CacheConfig) GetDefaultTTL() time.Duration {
	d, err := time.ParseDuration(c.DefaultTTL)
	if err != nil {
		return 1 * time.Hour
	}
	return d
}

func (c *CacheConfig) GetNegativeTTL() time.Duration {
	d, err := time.ParseDuration(c.NegativeTTL)
	if err != nil {
		return 10 * time.Second
	}
	return d
}

func (p *PersistenceConfig) GetAutoSaveInterval() time.Duration {
	d, err := time.ParseDuration(p.AutoSaveInterval)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

// GetEffectiveAppLevel returns the effective application log level
// Handles backward compatibility with legacy Level field
func (l *LoggingConfig) GetEffectiveAppLevel() string {
	if l.AppLevel != "" {
		return l.AppLevel
	}
	if l.Level != "" {
		return l.Level
	}
	return ""
}

// GetEffectiveAppLogfile returns the effective application log file
// Handles backward compatibility with legacy File field
func (l *LoggingConfig) GetEffectiveAppLogfile() string {
	if l.AppLogfile != "" {
		return l.AppLogfile
	}
	return l.File
}

// ValidateAccessFormat validates the access log format
func (l *LoggingConfig) ValidateAccessFormat() string {
	switch l.AccessFormat {
	case "human", "json":
		return l.AccessFormat
	case "":
		return "human" // default
	default:
		slog.Warn("config: invalid access_format, using default", "invalid", l.AccessFormat, "default", "human")
		return "human"
	}
}

// ApplyProcessDetection applies proper process detection for AccessToStdout
// This should be called after config loading to set the appropriate default
func (l *LoggingConfig) ApplyProcessDetection(isForeground bool) {
	// Set AccessToStdout based on process mode detection
	// This provides a sensible default: stdout for foreground, no stdout for daemon
	l.AccessToStdout = isForeground
}

func NewDefaultConfig() *Config {
	configDir, _ := os.UserConfigDir()
	gocacheDir := filepath.Join(configDir, "gocache")

	return &Config{
		Server: ServerConfig{
			ProxyPort:           8080,
			ControlPort:         8081,
			BindAddress:         "127.0.0.1",
			MaxCertCacheEntries: 1000,
		},
		Cache: CacheConfig{
			DefaultTTL:    "1h",
			NegativeTTL:   "10s",
			MaxSizeMB:     500,
			IgnoreNoCache: false,
			CacheableTypes: []string{
				"text/html",
				"text/css",
				"application/javascript",
				"application/json",
				"text/plain",
			},
			PostCache: PostCacheConfig{
				Enable:                false,
				IncludeQueryString:    false,
				MaxRequestBodySizeMB:  10,
				MaxResponseBodySizeMB: 10,
			},
		},
		Logging: LoggingConfig{
			// Legacy fields (kept for backward compatibility)
			Level: "", // Application logging disabled by default
			File:  "",

			// New logging configuration with proper defaults
			AppLevel:       "", // Application logging disabled by default
			AppLogfile:     "",
			AccessToStdout: true, // Will be set properly by ApplyProcessDetection()
			AccessLogfile:  "",
			AccessFormat:   "human",
		},
		Persistence: PersistenceConfig{
			Enable:           false,
			CacheFile:        filepath.Join(gocacheDir, "cache.gob"),
			AutoSaveInterval: "5m",
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := NewDefaultConfig()

	configPath := path
	if configPath == "" {
		// Search standard locations only if no path is provided.
		locations := []string{
			"./gocache.toml",
			os.ExpandEnv("$HOME/.config/gocache/config.toml"),
			os.ExpandEnv("$HOME/.gocache.toml"),
			"/etc/gocache/config.toml",
		}
		for _, loc := range locations {
			if _, err := os.Stat(loc); err == nil {
				configPath = loc
				break
			}
		}
	}

	// If a path was provided (or found), try to load it.
	if configPath != "" {
		if _, err := toml.DecodeFile(configPath, cfg); err != nil {
			return nil, err
		}
		cfg.LoadedPath = configPath
	}

	// Validate PostCache sizes
	if cfg.Cache.PostCache.MaxRequestBodySizeMB > MaxPostCacheBodySizeMB {
		slog.Warn("config: max_request_body_size_mb exceeds hard limit", "limit_mb", MaxPostCacheBodySizeMB, "configured_mb", cfg.Cache.PostCache.MaxRequestBodySizeMB)
		cfg.Cache.PostCache.MaxRequestBodySizeMB = MaxPostCacheBodySizeMB
	}
	if cfg.Cache.PostCache.MaxResponseBodySizeMB > MaxPostCacheBodySizeMB {
		slog.Warn("config: max_response_body_size_mb exceeds hard limit", "limit_mb", MaxPostCacheBodySizeMB, "configured_mb", cfg.Cache.PostCache.MaxResponseBodySizeMB)
		cfg.Cache.PostCache.MaxResponseBodySizeMB = MaxPostCacheBodySizeMB
	}

	// Validate logging configuration
	if cfg.Logging.GetEffectiveAppLevel() != "" {
		validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
		if !validLevels[cfg.Logging.GetEffectiveAppLevel()] {
			slog.Warn("config: invalid app_level, disabling application logging", "invalid", cfg.Logging.GetEffectiveAppLevel())
			cfg.Logging.AppLevel = ""
		}
	}

	// Validate access format
	cfg.Logging.AccessFormat = cfg.Logging.ValidateAccessFormat()

	// If no config file was loaded, cfg will just be the defaults.
	return cfg, nil
}
