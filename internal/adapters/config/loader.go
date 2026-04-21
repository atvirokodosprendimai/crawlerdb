package config

import (
	"fmt"
	"os"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/pelletier/go-toml/v2"
)

// LoadDefault returns configuration with all defaults.
func LoadDefault() valueobj.AppConfig {
	return valueobj.DefaultAppConfig()
}

// LoadFromFile reads a TOML configuration file and returns the parsed config.
// Values not present in the file retain their defaults.
func LoadFromFile(path string) (valueobj.AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return valueobj.AppConfig{}, fmt.Errorf("read config file: %w", err)
	}

	cfg := valueobj.DefaultAppConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return valueobj.AppConfig{}, fmt.Errorf("parse config file: %w", err)
	}

	return cfg, nil
}

// GenerateDefault writes the default configuration to a TOML file.
func GenerateDefault(path string) error {
	cfg := valueobj.DefaultAppConfig()

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal default config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}
