package setup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"clawkangsar/internal/auth"
	"clawkangsar/internal/config"
)

const codexBaseURL = "https://chatgpt.com/backend-api/codex"

type Options struct {
	ConfigPath string
	Profile    string
	Force      bool
}

type wizard struct {
	in     *bufio.Reader
	stdout io.Writer
	stderr io.Writer
}

func Run(opts Options, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	w := &wizard{
		in:     bufio.NewReader(stdin),
		stdout: stdout,
		stderr: stderr,
	}
	return w.run(opts)
}

func AvailableProfiles() []string {
	return []string{"systemd-first", "docker-first", "home-assistant"}
}

func (w *wizard) run(opts Options) error {
	configPath := strings.TrimSpace(opts.ConfigPath)
	if configPath == "" {
		configPath = "config.json"
	}

	fmt.Fprintln(w.stdout, "ClawKangsar setup")
	fmt.Fprintln(w.stdout, "This wizard writes a config file for Raspberry Pi deployment.")
	fmt.Fprintln(w.stdout)

	profile, err := w.selectProfile(opts.Profile)
	if err != nil {
		return err
	}

	cfg, err := profileConfig(profile)
	if err != nil {
		return err
	}

	fmt.Fprintf(w.stdout, "Profile: %s\n", profile)
	fmt.Fprintf(w.stdout, "Config path: %s\n\n", configPath)

	if err := w.configureGateways(&cfg); err != nil {
		return err
	}
	if err := w.configureLLM(&cfg); err != nil {
		return err
	}
	if err := w.configureTools(&cfg); err != nil {
		return err
	}
	if err := w.configureHealth(&cfg); err != nil {
		return err
	}

	backupPath, err := prepareConfigPath(configPath, opts.Force, w)
	if err != nil {
		return err
	}

	if err := config.Save(configPath, cfg); err != nil {
		return err
	}

	fmt.Fprintln(w.stdout)
	fmt.Fprintf(w.stdout, "Wrote config: %s\n", configPath)
	if backupPath != "" {
		fmt.Fprintf(w.stdout, "Backup created: %s\n", backupPath)
	}
	printWarnings(w.stdout, cfg)

	return nil
}

func (w *wizard) selectProfile(preselected string) (string, error) {
	preselected = strings.TrimSpace(preselected)
	if preselected != "" {
		if _, err := profileConfig(preselected); err != nil {
			return "", err
		}
		return preselected, nil
	}

	options := AvailableProfiles()
	return w.promptChoice("Starter profile", options, 0)
}

func (w *wizard) configureGateways(cfg *config.Config) error {
	fmt.Fprintln(w.stdout, "Gateway setup")

	telegramEnabled, err := w.promptYesNo("Enable Telegram gateway", true)
	if err != nil {
		return err
	}
	cfg.Telegram.Enabled = telegramEnabled
	if telegramEnabled {
		token, err := w.promptLine("Telegram bot token", cfg.Telegram.Token)
		if err != nil {
			return err
		}
		cfg.Telegram.Token = token

		allowList, err := w.promptInt64List("Telegram allow-list user IDs", cfg.Telegram.AllowList)
		if err != nil {
			return err
		}
		cfg.Telegram.AllowList = allowList
	} else {
		cfg.Telegram.Token = ""
		cfg.Telegram.AllowList = []int64{}
	}

	whatsAppEnabled, err := w.promptYesNo("Enable WhatsApp gateway", false)
	if err != nil {
		return err
	}
	cfg.WhatsApp.Enabled = whatsAppEnabled
	if whatsAppEnabled {
		sessionDSN, err := w.promptLine("WhatsApp SQLite session DSN", cfg.WhatsApp.SessionDSN)
		if err != nil {
			return err
		}
		cfg.WhatsApp.SessionDSN = sessionDSN
	}

	fmt.Fprintln(w.stdout)
	return nil
}

func (w *wizard) configureLLM(cfg *config.Config) error {
	fmt.Fprintln(w.stdout, "LLM setup")

	options := []string{"disabled", "codex_oauth", "openai_compat"}
	defaultIndex := 1
	if cfg.LLM.Enabled {
		switch cfg.LLM.Provider {
		case "openai_compat":
			defaultIndex = 2
		case "codex_oauth":
			defaultIndex = 1
		}
	} else {
		defaultIndex = 0
	}

	provider, err := w.promptChoice("LLM provider", options, defaultIndex)
	if err != nil {
		return err
	}

	switch provider {
	case "disabled":
		cfg.LLM.Enabled = false
		fmt.Fprintln(w.stdout)
		return nil
	case "codex_oauth":
		cfg.LLM.Enabled = true
		cfg.LLM.Provider = "codex_oauth"
		cfg.LLM.AuthMethod = "codex_cli"
		cfg.LLM.BaseURL = codexBaseURL
		model, err := w.promptRequired("Codex/OpenAI model name", cfg.LLM.Model)
		if err != nil {
			return err
		}
		cfg.LLM.Model = model
		authPath, err := w.promptLine("Codex auth path (blank for default ~/.codex/auth.json)", cfg.LLM.CodexAuthPath)
		if err != nil {
			return err
		}
		cfg.LLM.CodexAuthPath = authPath

		loginNow, err := w.promptYesNo("Run `clawkangsar auth codex` flow now", false)
		if err != nil {
			return err
		}
		if loginNow {
			if err := auth.EnsureCodexLogin(cfg.LLM.CodexAuthPath, false, w.stdout, w.stderr); err != nil {
				return err
			}
		}
	case "openai_compat":
		cfg.LLM.Enabled = true
		cfg.LLM.Provider = "openai_compat"
		cfg.LLM.AuthMethod = "api_key"
		baseURL, err := w.promptLine("OpenAI-compatible base URL", fallbackString(cfg.LLM.BaseURL, "https://api.openai.com/v1"))
		if err != nil {
			return err
		}
		cfg.LLM.BaseURL = baseURL

		model, err := w.promptRequired("Model name", cfg.LLM.Model)
		if err != nil {
			return err
		}
		cfg.LLM.Model = model

		useEnv, err := w.promptYesNo("Use environment variable for API key", true)
		if err != nil {
			return err
		}
		if useEnv {
			envName, err := w.promptLine("API key environment variable", fallbackString(cfg.LLM.APIKeyEnv, "OPENAI_API_KEY"))
			if err != nil {
				return err
			}
			cfg.LLM.APIKeyEnv = envName
			cfg.LLM.APIKey = ""
		} else {
			key, err := w.promptRequired("API key", cfg.LLM.APIKey)
			if err != nil {
				return err
			}
			cfg.LLM.APIKey = key
		}
		cfg.LLM.CodexAuthPath = ""
	}

	fmt.Fprintln(w.stdout)
	return nil
}

func (w *wizard) configureTools(cfg *config.Config) error {
	fmt.Fprintln(w.stdout, "Tool setup")

	shellEnabled, err := w.promptYesNo("Enable named shell command aliases", cfg.Tools.ShellEnabled)
	if err != nil {
		return err
	}
	cfg.Tools.ShellEnabled = shellEnabled
	if shellEnabled && len(cfg.Tools.ShellCommands) > 0 {
		fmt.Fprintf(w.stdout, "Shell aliases: %s\n", strings.Join(sortedMapKeys(cfg.Tools.ShellCommands), ", "))
	}

	systemctlEnabled, err := w.promptYesNo("Enable systemctl tools", cfg.Tools.SystemctlEnabled)
	if err != nil {
		return err
	}
	cfg.Tools.SystemctlEnabled = systemctlEnabled
	if systemctlEnabled {
		services, err := w.promptStringList("Allow-listed systemd services", cfg.Tools.SystemctlAllowServices)
		if err != nil {
			return err
		}
		cfg.Tools.SystemctlAllowServices = services
	} else {
		cfg.Tools.SystemctlAllowServices = []string{}
	}

	dockerEnabled, err := w.promptYesNo("Enable Docker tools", cfg.Tools.DockerEnabled)
	if err != nil {
		return err
	}
	cfg.Tools.DockerEnabled = dockerEnabled
	if dockerEnabled {
		containers, err := w.promptStringList("Allow-listed Docker containers for logs", cfg.Tools.DockerAllowContainers)
		if err != nil {
			return err
		}
		cfg.Tools.DockerAllowContainers = containers
	} else {
		cfg.Tools.DockerAllowContainers = []string{}
	}

	journalEnabled, err := w.promptYesNo("Enable journalctl log tailing", cfg.Tools.JournalEnabled)
	if err != nil {
		return err
	}
	cfg.Tools.JournalEnabled = journalEnabled
	if journalEnabled {
		units, err := w.promptStringList("Allow-listed journal units", cfg.Tools.JournalAllowUnits)
		if err != nil {
			return err
		}
		cfg.Tools.JournalAllowUnits = units
	} else {
		cfg.Tools.JournalAllowUnits = []string{}
	}

	fmt.Fprintln(w.stdout)
	return nil
}

func (w *wizard) configureHealth(cfg *config.Config) error {
	fmt.Fprintln(w.stdout, "Health endpoint setup")

	enabled, err := w.promptYesNo("Enable health server", cfg.Health.Enabled)
	if err != nil {
		return err
	}
	cfg.Health.Enabled = enabled
	if !enabled {
		fmt.Fprintln(w.stdout)
		return nil
	}

	host, err := w.promptLine("Health bind host", cfg.Health.Host)
	if err != nil {
		return err
	}
	cfg.Health.Host = host

	port, err := w.promptInt("Health port", cfg.Health.Port)
	if err != nil {
		return err
	}
	cfg.Health.Port = port

	fmt.Fprintln(w.stdout)
	return nil
}

func profileConfig(name string) (config.Config, error) {
	cfg := config.Default()

	switch strings.ToLower(strings.TrimSpace(name)) {
	case "systemd-first":
		cfg.Tools.ShellEnabled = true
		cfg.Tools.ShellCommands = map[string]string{
			"uptime":    "uptime",
			"disk_free": "df -h /",
			"mem_free":  "free -m",
			"cpu_temp":  "vcgencmd measure_temp || cat /sys/class/thermal/thermal_zone0/temp",
		}
		cfg.Tools.SystemctlEnabled = true
		cfg.Tools.SystemctlAllowServices = []string{"clawkangsar.service", "tailscaled.service", "caddy.service"}
		cfg.Tools.JournalEnabled = true
		cfg.Tools.JournalAllowUnits = []string{"clawkangsar.service", "tailscaled.service", "caddy.service"}
	case "docker-first":
		cfg.Tools.ShellEnabled = true
		cfg.Tools.ShellCommands = map[string]string{
			"uptime":        "uptime",
			"disk_free":     "df -h /",
			"mem_free":      "free -m",
			"docker_health": "docker info --format '{{.ServerVersion}}'",
		}
		cfg.Tools.SystemctlEnabled = true
		cfg.Tools.SystemctlAllowServices = []string{"clawkangsar.service", "docker.service"}
		cfg.Tools.DockerEnabled = true
		cfg.Tools.DockerAllowContainers = []string{"homeassistant", "traefik", "portainer"}
		cfg.Tools.JournalEnabled = true
		cfg.Tools.JournalAllowUnits = []string{"clawkangsar.service", "docker.service"}
	case "home-assistant":
		cfg.Tools.ShellEnabled = true
		cfg.Tools.DefaultLogLines = 100
		cfg.Tools.MaxLogLines = 250
		cfg.Tools.ShellCommands = map[string]string{
			"uptime":     "uptime",
			"disk_free":  "df -h /",
			"mem_free":   "free -m",
			"cpu_temp":   "vcgencmd measure_temp || cat /sys/class/thermal/thermal_zone0/temp",
			"mqtt_check": "ss -ltnp | grep 1883 || true",
		}
		cfg.Tools.SystemctlEnabled = true
		cfg.Tools.SystemctlAllowServices = []string{
			"clawkangsar.service",
			"home-assistant@homeassistant.service",
			"zigbee2mqtt.service",
			"mosquitto.service",
			"node-red.service",
		}
		cfg.Tools.DockerEnabled = true
		cfg.Tools.DockerAllowContainers = []string{"homeassistant", "zigbee2mqtt", "mosquitto", "nodered"}
		cfg.Tools.JournalEnabled = true
		cfg.Tools.JournalAllowUnits = []string{
			"clawkangsar.service",
			"home-assistant@homeassistant.service",
			"zigbee2mqtt.service",
			"mosquitto.service",
			"node-red.service",
		}
	default:
		return config.Config{}, fmt.Errorf("unknown profile %q; available: %s", name, strings.Join(AvailableProfiles(), ", "))
	}

	return cfg, nil
}

func prepareConfigPath(path string, force bool, w *wizard) (string, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	if !force {
		ok, err := w.promptYesNo("Config exists. Back up and overwrite", false)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("setup cancelled")
		}
	}

	backupPath := fmt.Sprintf("%s.bak.%s", path, time.Now().Format("20060102-150405"))
	input, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read existing config: %w", err)
	}

	dir := filepath.Dir(backupPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create backup directory: %w", err)
		}
	}

	if err := os.WriteFile(backupPath, input, 0o644); err != nil {
		return "", fmt.Errorf("write backup config: %w", err)
	}
	return backupPath, nil
}

func printWarnings(out io.Writer, cfg config.Config) {
	warnings := make([]string, 0, 4)
	if cfg.Telegram.Enabled && strings.TrimSpace(cfg.Telegram.Token) == "" {
		warnings = append(warnings, "Telegram is enabled but token is empty.")
	}
	if cfg.Telegram.Enabled && len(cfg.Telegram.AllowList) == 0 {
		warnings = append(warnings, "Telegram is enabled but allow_list is empty.")
	}
	if cfg.LLM.Enabled && strings.TrimSpace(cfg.LLM.Model) == "" {
		warnings = append(warnings, "LLM is enabled but model is empty.")
	}
	if cfg.LLM.Enabled && cfg.LLM.Provider == "openai_compat" && strings.TrimSpace(cfg.LLM.APIKey) == "" && strings.TrimSpace(cfg.LLM.APIKeyEnv) == "" {
		warnings = append(warnings, "OpenAI-compatible LLM is enabled but no API key or environment variable is configured.")
	}

	if len(warnings) == 0 {
		fmt.Fprintln(out, "Config looks complete enough to start.")
		return
	}

	fmt.Fprintln(out, "Warnings:")
	for _, warning := range warnings {
		fmt.Fprintf(out, "- %s\n", warning)
	}
}

func (w *wizard) promptLine(label string, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(w.stdout, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(w.stdout, "%s: ", label)
	}
	text, err := w.readLine()
	if err != nil {
		return "", err
	}
	if text == "" {
		return defaultValue, nil
	}
	return text, nil
}

func (w *wizard) promptRequired(label string, defaultValue string) (string, error) {
	for {
		value, err := w.promptLine(label, defaultValue)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(value) != "" {
			return value, nil
		}
		fmt.Fprintln(w.stdout, "A value is required.")
	}
}

func (w *wizard) promptYesNo(label string, defaultValue bool) (bool, error) {
	suffix := "[y/N]"
	if defaultValue {
		suffix = "[Y/n]"
	}

	for {
		fmt.Fprintf(w.stdout, "%s %s: ", label, suffix)
		text, err := w.readLine()
		if err != nil {
			return false, err
		}
		if text == "" {
			return defaultValue, nil
		}

		switch strings.ToLower(text) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(w.stdout, "Enter yes or no.")
		}
	}
}

func (w *wizard) promptChoice(label string, options []string, defaultIndex int) (string, error) {
	fmt.Fprintf(w.stdout, "%s:\n", label)
	for i, option := range options {
		marker := " "
		if i == defaultIndex {
			marker = "*"
		}
		fmt.Fprintf(w.stdout, "  %s %d. %s\n", marker, i+1, option)
	}

	for {
		fmt.Fprintf(w.stdout, "Select option [%d]: ", defaultIndex+1)
		text, err := w.readLine()
		if err != nil {
			return "", err
		}
		if text == "" {
			return options[defaultIndex], nil
		}

		index, err := strconv.Atoi(text)
		if err != nil || index < 1 || index > len(options) {
			fmt.Fprintln(w.stdout, "Enter a valid option number.")
			continue
		}
		return options[index-1], nil
	}
}

func (w *wizard) promptStringList(label string, defaultValues []string) ([]string, error) {
	defaultText := strings.Join(defaultValues, ",")
	value, err := w.promptLine(label+" (comma-separated)", defaultText)
	if err != nil {
		return nil, err
	}
	return splitCSV(value), nil
}

func (w *wizard) promptInt64List(label string, defaultValues []int64) ([]int64, error) {
	defaultParts := make([]string, 0, len(defaultValues))
	for _, item := range defaultValues {
		defaultParts = append(defaultParts, strconv.FormatInt(item, 10))
	}

	for {
		value, err := w.promptLine(label+" (comma-separated)", strings.Join(defaultParts, ","))
		if err != nil {
			return nil, err
		}
		parts := splitCSV(value)
		if len(parts) == 0 {
			return []int64{}, nil
		}

		items := make([]int64, 0, len(parts))
		valid := true
		for _, part := range parts {
			item, err := strconv.ParseInt(part, 10, 64)
			if err != nil {
				fmt.Fprintf(w.stdout, "Invalid numeric ID: %s\n", part)
				valid = false
				break
			}
			items = append(items, item)
		}
		if valid {
			return items, nil
		}
	}
}

func (w *wizard) promptInt(label string, defaultValue int) (int, error) {
	for {
		value, err := w.promptLine(label, strconv.Itoa(defaultValue))
		if err != nil {
			return 0, err
		}
		item, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || item <= 0 {
			fmt.Fprintln(w.stdout, "Enter a positive integer.")
			continue
		}
		return item, nil
	}
}

func (w *wizard) readLine() (string, error) {
	text, err := w.in.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func fallbackString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
