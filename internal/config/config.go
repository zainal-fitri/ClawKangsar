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
	LLM          LLMConfig      `json:"llm"`
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

type LLMConfig struct {
	Enabled         bool    `json:"enabled"`
	Provider        string  `json:"provider"`
	AuthMethod      string  `json:"auth_method"`
	BaseURL         string  `json:"base_url"`
	APIKey          string  `json:"api_key"`
	APIKeyEnv       string  `json:"api_key_env"`
	CodexAuthPath   string  `json:"codex_auth_path"`
	Model           string  `json:"model"`
	Temperature     float64 `json:"temperature"`
	MaxTokens       int     `json:"max_tokens"`
	TimeoutSeconds  int     `json:"timeout_seconds"`
	HistoryMessages int     `json:"history_messages"`
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
	WebFetchTimeoutSeconds int               `json:"web_fetch_timeout_seconds"`
	WebFetchMaxChars       int               `json:"web_fetch_max_chars"`
	CommandTimeoutSeconds  int               `json:"command_timeout_seconds"`
	DefaultLogLines        int               `json:"default_log_lines"`
	MaxLogLines            int               `json:"max_log_lines"`
	ShellEnabled           bool              `json:"shell_enabled"`
	ShellCommands          map[string]string `json:"shell_commands"`
	SystemctlEnabled       bool              `json:"systemctl_enabled"`
	SystemctlAllowServices []string          `json:"systemctl_allow_services"`
	DockerEnabled          bool              `json:"docker_enabled"`
	DockerAllowContainers  []string          `json:"docker_allow_containers"`
	JournalEnabled         bool              `json:"journal_enabled"`
	JournalAllowUnits      []string          `json:"journal_allow_units"`
}

func Default() Config {
	return Config{
		LogLevel:     "INFO",
		SystemPrompt: defaultSystemPrompt,
		LLM: LLMConfig{
			Enabled:         false,
			Provider:        "openai_compat",
			AuthMethod:      "api_key",
			BaseURL:         "https://api.openai.com/v1",
			APIKey:          "",
			APIKeyEnv:       "OPENAI_API_KEY",
			CodexAuthPath:   "",
			Model:           "",
			Temperature:     0.2,
			MaxTokens:       512,
			TimeoutSeconds:  60,
			HistoryMessages: 16,
		},
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
			CommandTimeoutSeconds:  20,
			DefaultLogLines:        80,
			MaxLogLines:            200,
			ShellEnabled:           false,
			ShellCommands:          map[string]string{},
			SystemctlEnabled:       false,
			SystemctlAllowServices: []string{},
			DockerEnabled:          false,
			DockerAllowContainers:  []string{},
			JournalEnabled:         false,
			JournalAllowUnits:      []string{},
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
	if c.LLM.Provider == "" {
		c.LLM.Provider = defaults.LLM.Provider
	}
	if c.LLM.AuthMethod == "" {
		c.LLM.AuthMethod = defaults.LLM.AuthMethod
	}
	if c.LLM.BaseURL == "" {
		c.LLM.BaseURL = defaults.LLM.BaseURL
	}
	if c.LLM.APIKeyEnv == "" {
		c.LLM.APIKeyEnv = defaults.LLM.APIKeyEnv
	}
	if c.LLM.TimeoutSeconds <= 0 {
		c.LLM.TimeoutSeconds = defaults.LLM.TimeoutSeconds
	}
	if c.LLM.MaxTokens <= 0 {
		c.LLM.MaxTokens = defaults.LLM.MaxTokens
	}
	if c.LLM.HistoryMessages <= 0 {
		c.LLM.HistoryMessages = defaults.LLM.HistoryMessages
	}
	if c.LLM.Temperature < 0 {
		c.LLM.Temperature = defaults.LLM.Temperature
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
	if c.Tools.CommandTimeoutSeconds <= 0 {
		c.Tools.CommandTimeoutSeconds = defaults.Tools.CommandTimeoutSeconds
	}
	if c.Tools.DefaultLogLines <= 0 {
		c.Tools.DefaultLogLines = defaults.Tools.DefaultLogLines
	}
	if c.Tools.MaxLogLines <= 0 {
		c.Tools.MaxLogLines = defaults.Tools.MaxLogLines
	}
	if c.Tools.MaxLogLines < c.Tools.DefaultLogLines {
		c.Tools.MaxLogLines = c.Tools.DefaultLogLines
	}
	if c.Tools.ShellCommands == nil {
		c.Tools.ShellCommands = map[string]string{}
	}
	if c.Tools.SystemctlAllowServices == nil {
		c.Tools.SystemctlAllowServices = []string{}
	}
	if c.Tools.DockerAllowContainers == nil {
		c.Tools.DockerAllowContainers = []string{}
	}
	if c.Tools.JournalAllowUnits == nil {
		c.Tools.JournalAllowUnits = []string{}
	}
	if c.Telegram.AllowList == nil {
		c.Telegram.AllowList = []int64{}
	}
}
