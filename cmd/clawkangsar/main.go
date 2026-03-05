package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"clawkangsar/internal/auth"
	"clawkangsar/internal/config"
	"clawkangsar/internal/core"
	"clawkangsar/internal/gateway/telegram"
	"clawkangsar/internal/gateway/whatsapp"
	"clawkangsar/internal/health"
	"clawkangsar/internal/llm"
	"clawkangsar/internal/setup"
	"clawkangsar/internal/tools"
	"clawkangsar/internal/version"
)

type runner struct {
	name  string
	start func(ctx context.Context) error
}

type gatewayRuntime struct {
	Configured bool      `json:"configured"`
	Running    bool      `json:"running"`
	LastError  string    `json:"last_error,omitempty"`
	LastChange time.Time `json:"last_change"`
}

type statusTracker struct {
	mu       sync.RWMutex
	app      string
	version  string
	started  time.Time
	gateways map[string]*gatewayRuntime
	agent    *core.Agent
	browser  *tools.Browser
}

func main() {
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "auth":
			if err := runAuthCommand(args[1:]); err != nil {
				slog.Error("auth command failed", "error", err)
				os.Exit(1)
			}
			return
		case "setup":
			if err := runSetupCommand(args[1:]); err != nil {
				slog.Error("setup command failed", "error", err)
				os.Exit(1)
			}
			return
		default:
			slog.Error("unknown command", "command", args[0])
			os.Exit(1)
		}
	}

	configPath := flag.String("config", "config.json", "Path to configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "path", *configPath, "error", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)
	logger.Info("starting service", "app", version.AppName, "version", version.Version)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	sessionStore, err := core.NewSessionStore(cfg.Storage.SessionDir)
	if err != nil {
		logger.Error("failed to initialize session store", "path", cfg.Storage.SessionDir, "error", err)
		os.Exit(1)
	}

	browser := tools.NewBrowser(logger.With("component", "browser"), time.Duration(cfg.Browser.IdleTimeoutSeconds)*time.Second)
	defer browser.Close()

	webFetcher := tools.NewWebFetcher(
		logger.With("component", "web_fetch"),
		time.Duration(cfg.Tools.WebFetchTimeoutSeconds)*time.Second,
		cfg.Tools.WebFetchMaxChars,
	)
	serverControl := tools.NewServerControl(logger.With("component", "server_tools"), tools.ServerControlOptions{
		TimeoutSeconds:         cfg.Tools.CommandTimeoutSeconds,
		DefaultLogLines:        cfg.Tools.DefaultLogLines,
		MaxLogLines:            cfg.Tools.MaxLogLines,
		ShellEnabled:           cfg.Tools.ShellEnabled,
		ShellCommands:          cfg.Tools.ShellCommands,
		SystemctlEnabled:       cfg.Tools.SystemctlEnabled,
		SystemctlAllowServices: cfg.Tools.SystemctlAllowServices,
		DockerEnabled:          cfg.Tools.DockerEnabled,
		DockerAllowContainers:  cfg.Tools.DockerAllowContainers,
		JournalEnabled:         cfg.Tools.JournalEnabled,
		JournalAllowUnits:      cfg.Tools.JournalAllowUnits,
	})

	var provider core.ChatProvider
	if cfg.LLM.Enabled {
		switch strings.ToLower(strings.TrimSpace(cfg.LLM.Provider)) {
		case "openai_compat", "openai-compatible", "openai":
			provider, err = llm.NewOpenAICompatProvider(cfg.LLM)
			if err != nil {
				logger.Error("failed to initialize llm provider", "provider", cfg.LLM.Provider, "error", err)
				os.Exit(1)
			}
		case "codex_oauth", "openai_oauth", "codex":
			provider, err = llm.NewCodexOAuthProvider(cfg.LLM)
			if err != nil {
				logger.Error("failed to initialize llm provider", "provider", cfg.LLM.Provider, "error", err)
				os.Exit(1)
			}
		default:
			logger.Error("unsupported llm provider", "provider", cfg.LLM.Provider)
			os.Exit(1)
		}
	}

	agent := core.NewAgent(cfg.SystemPrompt, browser, webFetcher, serverControl, provider, sessionStore)

	runners, err := buildRunners(cfg, agent, logger)
	if err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}
	if len(runners) == 0 {
		logger.Warn("no gateway enabled; set whatsapp.enabled or telegram.enabled in config.json")
	}

	tracker := newStatusTracker(version.AppName, version.Version, runners, agent, browser)

	var wg sync.WaitGroup

	if cfg.Health.Enabled {
		healthServer := health.NewServer(
			cfg.Health.Host,
			cfg.Health.Port,
			tracker.snapshot,
			tracker.ready,
			logger.With("component", "health"),
		)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := healthServer.Start(ctx); err != nil {
				logger.Error("health server stopped with error", "error", err)
			}
		}()
	}

	for _, gatewayRunner := range runners {
		gatewayRunner := gatewayRunner
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.setRunning(gatewayRunner.name, true)

			if err := gatewayRunner.start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				tracker.setRunning(gatewayRunner.name, false)
				tracker.setError(gatewayRunner.name, err)
				logger.Error("gateway stopped with error", "gateway", gatewayRunner.name, "error", err)
				return
			}

			tracker.setRunning(gatewayRunner.name, false)
		}()
	}

	<-ctx.Done()
	logger.Info("shutdown signal received", "app", version.AppName)
	wg.Wait()
	logger.Info("service stopped", "app", version.AppName)
}

func runAuthCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("missing auth target; supported: codex")
	}

	switch args[0] {
	case "codex":
		fs := flag.NewFlagSet("auth codex", flag.ContinueOnError)
		fs.SetOutput(os.Stdout)
		force := fs.Bool("force", false, "Force interactive Codex re-login")
		codexAuthPath := fs.String("codex-auth-path", "", "Override path to Codex auth.json")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		return auth.EnsureCodexLogin(*codexAuthPath, *force, os.Stdout, os.Stderr)
	default:
		return fmt.Errorf("unsupported auth target: %s", args[0])
	}
}

func runSetupCommand(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "config.json", "Path to write configuration file")
	profile := fs.String("profile", "", "Starter profile: systemd-first, docker-first, home-assistant")
	force := fs.Bool("force", false, "Overwrite existing config after automatic backup")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	return setup.Run(setup.Options{
		ConfigPath: *configPath,
		Profile:    *profile,
		Force:      *force,
	}, os.Stdin, os.Stdout, os.Stderr)
}

func buildRunners(cfg config.Config, processor core.Processor, logger *slog.Logger) ([]runner, error) {
	runners := make([]runner, 0, 2)

	if cfg.Telegram.Enabled {
		tgGateway, err := telegram.New(cfg.Telegram, processor, logger.With("gateway", "telegram"))
		if err != nil {
			return nil, err
		}
		runners = append(runners, runner{
			name:  "telegram",
			start: tgGateway.Start,
		})
	}

	if cfg.WhatsApp.Enabled {
		waGateway, err := whatsapp.New(cfg.WhatsApp, processor, logger.With("gateway", "whatsapp"))
		if err != nil {
			return nil, err
		}
		runners = append(runners, runner{
			name:  "whatsapp",
			start: waGateway.Start,
		})
	}

	return runners, nil
}

func newStatusTracker(app string, appVersion string, runners []runner, agent *core.Agent, browser *tools.Browser) *statusTracker {
	gateways := make(map[string]*gatewayRuntime, len(runners))
	for _, r := range runners {
		gateways[r.name] = &gatewayRuntime{
			Configured: true,
			Running:    false,
			LastChange: time.Now(),
		}
	}

	return &statusTracker{
		app:      app,
		version:  appVersion,
		started:  time.Now(),
		gateways: gateways,
		agent:    agent,
		browser:  browser,
	}
}

func (s *statusTracker) setRunning(name string, running bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.gateways[name]
	if !exists {
		state = &gatewayRuntime{Configured: true}
		s.gateways[name] = state
	}

	state.Running = running
	state.LastChange = time.Now()
	if running {
		state.LastError = ""
	}
}

func (s *statusTracker) setError(name string, err error) {
	if err == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.gateways[name]
	if !exists {
		state = &gatewayRuntime{Configured: true}
		s.gateways[name] = state
	}

	state.LastError = err.Error()
	state.LastChange = time.Now()
}

func (s *statusTracker) ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configured := 0
	running := 0
	for _, gateway := range s.gateways {
		if gateway.Configured {
			configured++
		}
		if gateway.Running {
			running++
		}
	}

	if configured == 0 {
		return true
	}
	return running > 0
}

func (s *statusTracker) snapshot() map[string]any {
	s.mu.RLock()
	gateways := make(map[string]gatewayRuntime, len(s.gateways))
	for name, state := range s.gateways {
		gateways[name] = *state
	}
	s.mu.RUnlock()

	payload := map[string]any{
		"app":            s.app,
		"version":        s.version,
		"ready":          s.ready(),
		"started_at":     s.started,
		"uptime_seconds": int(time.Since(s.started).Seconds()),
		"gateways":       gateways,
	}

	if s.agent != nil {
		payload["agent"] = s.agent.Stats()
	}
	if s.browser != nil {
		payload["browser"] = s.browser.Stats()
	}

	return payload
}

func newLogger(level string) *slog.Logger {
	logLevel := slog.LevelInfo
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "DEBUG":
		logLevel = slog.LevelDebug
	case "WARN", "WARNING":
		logLevel = slog.LevelWarn
	case "ERROR":
		logLevel = slog.LevelError
	}

	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
}
