package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	SiteName     string `json:"site_name"`
	BaseURL      string `json:"base_url"`
	DefaultImage string `json:"default_image"`
	Author       string `json:"author"`
	LicenseName  string `json:"license_name"`
	LicenseURL   string `json:"license_url"`
}

func LoadConfig(configDir string, configJSON string) (*Config, error) {
	config := &Config{}

	if configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), config); err != nil {
			return nil, fmt.Errorf("error parsing config JSON: %w", err)
		}
		return config, nil
	}

	configPath := filepath.Join(configDir, ".oxen.json")
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("error parsing .oxen.json: %w", err)
		}
		return config, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("error reading .oxen.json: %w", err)
	}

	return config, nil
}
