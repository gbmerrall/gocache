package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server      ServerConfig      `toml:"server"`
	Cache       CacheConfig       `toml:"cache"`
	Logging     LoggingConfig     `toml:"logging"`
	Persistence PersistenceConfig `toml:"persistence"`
	LoadedPath  string            `toml:"-"` // To be populated after loading
}

type ServerConfig struct {
	ProxyPort   int    `toml:"proxy_port"`
	ControlPort int    `toml:"control_port"`
	BindAddress string `toml:"bind_address"`
}

type CacheConfig struct {
	DefaultTTL     string   `toml:"default_ttl"`
	MaxSizeMB      int      `toml:"max_size_mb"`
	IgnoreNoCache  bool     `toml:"ignore_no_cache"`
	CacheableTypes []string `toml:"cacheable_types"`
}

type LoggingConfig struct {
	Level string `toml:"level"`
	File  string `toml:"file"`
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

func (p *PersistenceConfig) GetAutoSaveInterval() time.Duration {
	d, err := time.ParseDuration(p.AutoSaveInterval)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

func NewDefaultConfig() *Config {
	configDir, _ := os.UserConfigDir()
	gocacheDir := filepath.Join(configDir, "gocache")

	return &Config{
		Server: ServerConfig{
			ProxyPort:   8080,
			ControlPort: 8081,
			BindAddress: "127.0.0.1",
		},
		Cache: CacheConfig{
			DefaultTTL:    "1h",
			MaxSizeMB:     500,
			IgnoreNoCache: false,
			CacheableTypes: []string{
				"text/html",
				"text/css",
				"application/javascript",
				"application/json",
				"text/plain",
			},
		},
		Logging: LoggingConfig{
			Level: "info",
			File:  "",
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

	// If no config file was loaded, cfg will just be the defaults.
	return cfg, nil
}
