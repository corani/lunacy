package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	DataDir string        `yaml:"data_dir"`
	Backup  BackupConfig  `yaml:"backup"`
}

type ServerConfig struct {
	Addr     string `yaml:"addr"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type BackupConfig struct {
	MaxCount int `yaml:"max_count"`
}

func defaults() Config {
	return Config{
		Server: ServerConfig{
			Addr:     ":8080",
			Username: "moon",
			Password: "secret",
		},
		DataDir: "$HOME/.local/share/lunacy",
		Backup: BackupConfig{
			MaxCount: 10,
		},
	}
}

// Load reads the config from path. If path is empty it searches
// ./config.yaml, $XDG_CONFIG_HOME/lunacy/config.yaml, and
// $HOME/.config/lunacy/config.yaml in order. Missing file is not an error.
func Load(path string) (Config, error) {
	cfg := defaults()

	if path == "" {
		path = findConfig()
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, err
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, err
		}
	}

	cfg.DataDir = os.ExpandEnv(cfg.DataDir)
	return cfg, nil
}

func findConfig() string {
	candidates := []string{"config.yaml"}

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "lunacy", "config.yaml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "lunacy", "config.yaml"))
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			return p
		}
	}
	return ""
}
