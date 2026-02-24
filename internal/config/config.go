package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

const defaultSystemPrompt = "You are ClawKangsar, a professional assistant running on a Raspberry Pi. Keep responses concise and use your browser tool only when real-time data is needed."

type Config struct {
	LogLevel     string         `json:"log_level"`
	SystemPrompt string         `json:"system_prompt"`
	WhatsApp     WhatsAppConfig `json:"whatsapp"`
	Telegram     TelegramConfig `json:"telegram"`
	Browser      BrowserConfig  `json:"browser"`
	Storage      StorageConfig  `json:"storage"`
	Health       HealthConfig   `json:"health"`
	Tools        ToolsConfig    `json:"tools"`
}

type WhatsAppConfig struct {
	Enabled    bool   `json:"enabled"`
	SessionDSN string `json:"session_dsn"`
}

type TelegramConfig struct {
	Enabled   bool    `json:"enabled"`
	Token     string  `json:"token"`
	AllowList []int64 `json:"allow_list"`
}

type BrowserConfig struct {
	IdleTimeoutSeconds int `json:"idle_timeout_seconds"`
}

type StorageConfig struct {
	SessionDir string `json:"session_dir"`
}

type HealthConfig struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

type ToolsConfig struct {
	WebFetchTimeoutSeconds int `json:"web_fetch_timeout_seconds"`
	WebFetchMaxChars       int `json:"web_fetch_max_chars"`
}

func Default() Config {
	return Config{
		LogLevel:     "INFO",
		SystemPrompt: defaultSystemPrompt,
		WhatsApp: WhatsAppConfig{
			Enabled:    false,
			SessionDSN: "file:clawkangsar_whatsapp.db?_foreign_keys=on",
		},
		Telegram: TelegramConfig{
			Enabled:   false,
			Token:     "",
			AllowList: []int64{},
		},
		Browser: BrowserConfig{
			IdleTimeoutSeconds: 300,
		},
		Storage: StorageConfig{
			SessionDir: "data/sessions",
		},
		Health: HealthConfig{
			Enabled: true,
			Host:    "0.0.0.0",
			Port:    18080,
		},
		Tools: ToolsConfig{
			WebFetchTimeoutSeconds: 20,
			WebFetchMaxChars:       4000,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()

	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("read config file: %w", err)
	}

	if err := json.Unmarshal(payload, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config file: %w", err)
	}

	cfg.Normalize()
	return cfg, nil
}

func (c *Config) Normalize() {
	defaults := Default()
	if c.LogLevel == "" {
		c.LogLevel = defaults.LogLevel
	}
	if c.SystemPrompt == "" {
		c.SystemPrompt = defaults.SystemPrompt
	}
	if c.WhatsApp.SessionDSN == "" {
		c.WhatsApp.SessionDSN = defaults.WhatsApp.SessionDSN
	}
	if c.Browser.IdleTimeoutSeconds <= 0 {
		c.Browser.IdleTimeoutSeconds = defaults.Browser.IdleTimeoutSeconds
	}
	if c.Storage.SessionDir == "" {
		c.Storage.SessionDir = defaults.Storage.SessionDir
	}
	if c.Health.Host == "" {
		c.Health.Host = defaults.Health.Host
	}
	if c.Health.Port <= 0 {
		c.Health.Port = defaults.Health.Port
	}
	if c.Tools.WebFetchTimeoutSeconds <= 0 {
		c.Tools.WebFetchTimeoutSeconds = defaults.Tools.WebFetchTimeoutSeconds
	}
	if c.Tools.WebFetchMaxChars <= 0 {
		c.Tools.WebFetchMaxChars = defaults.Tools.WebFetchMaxChars
	}
	if c.Telegram.AllowList == nil {
		c.Telegram.AllowList = []int64{}
	}
}
