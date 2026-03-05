package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxCommandOutputChars = 6000

type ServerControlOptions struct {
	TimeoutSeconds         int
	DefaultLogLines        int
	MaxLogLines            int
	ShellEnabled           bool
	ShellCommands          map[string]string
	SystemctlEnabled       bool
	SystemctlAllowServices []string
	DockerEnabled          bool
	DockerAllowContainers  []string
	JournalEnabled         bool
	JournalAllowUnits      []string
}

type ServerControl struct {
	logger *slog.Logger

	timeout         time.Duration
	defaultLogLines int
	maxLogLines     int

	shellEnabled  bool
	shellCommands map[string]string

	systemctlEnabled bool
	allowedServices  map[string]string

	dockerEnabled     bool
	allowedContainers map[string]string

	journalEnabled bool
	allowedUnits   map[string]string
}

func (s *ServerControl) ShellAvailable() bool {
	return s.shellEnabled && len(s.shellCommands) > 0
}

func (s *ServerControl) SystemctlAvailable() bool {
	return s.systemctlEnabled && len(s.allowedServices) > 0
}

func (s *ServerControl) DockerAvailable() bool {
	return s.dockerEnabled
}

func (s *ServerControl) JournalAvailable() bool {
	return s.journalEnabled && len(s.allowedUnits) > 0
}

func NewServerControl(logger *slog.Logger, opts ServerControlOptions) *ServerControl {
	if logger == nil {
		logger = slog.Default()
	}

	timeout := 20 * time.Second
	if opts.TimeoutSeconds > 0 {
		timeout = time.Duration(opts.TimeoutSeconds) * time.Second
	}

	defaultLogLines := opts.DefaultLogLines
	if defaultLogLines <= 0 {
		defaultLogLines = 80
	}

	maxLogLines := opts.MaxLogLines
	if maxLogLines <= 0 {
		maxLogLines = 200
	}
	if maxLogLines < defaultLogLines {
		maxLogLines = defaultLogLines
	}

	return &ServerControl{
		logger:            logger,
		timeout:           timeout,
		defaultLogLines:   defaultLogLines,
		maxLogLines:       maxLogLines,
		shellEnabled:      opts.ShellEnabled,
		shellCommands:     normalizeCommandMap(opts.ShellCommands),
		systemctlEnabled:  opts.SystemctlEnabled,
		allowedServices:   normalizeNameSet(opts.SystemctlAllowServices, true),
		dockerEnabled:     opts.DockerEnabled,
		allowedContainers: normalizeNameSet(opts.DockerAllowContainers, false),
		journalEnabled:    opts.JournalEnabled,
		allowedUnits:      normalizeNameSet(opts.JournalAllowUnits, true),
	}
}

func (s *ServerControl) ShellCommandNames() []string {
	if !s.ShellAvailable() {
		return nil
	}
	return sortedKeys(s.shellCommands)
}

func (s *ServerControl) AllowedServices() []string {
	if !s.SystemctlAvailable() {
		return nil
	}
	return sortedValues(s.allowedServices)
}

func (s *ServerControl) AllowedContainers() []string {
	if !s.DockerAvailable() || len(s.allowedContainers) == 0 {
		return nil
	}
	return sortedValues(s.allowedContainers)
}

func (s *ServerControl) AllowedUnits() []string {
	if !s.JournalAvailable() {
		return nil
	}
	return sortedValues(s.allowedUnits)
}

func (s *ServerControl) RunNamedCommand(ctx context.Context, name string) (string, error) {
	if !s.shellEnabled {
		return "", errors.New("named shell commands are disabled")
	}

	command, ok := s.shellCommands[normalizeLookup(name)]
	if !ok {
		return "", fmt.Errorf("named command %q is not allowed", strings.TrimSpace(name))
	}

	s.logger.Info("running named shell command", "name", strings.TrimSpace(name))
	if runtime.GOOS == "windows" {
		return s.run(ctx, "cmd.exe", "/C", command)
	}
	return s.run(ctx, "/bin/sh", "-lc", command)
}

func (s *ServerControl) SystemctlStatus(ctx context.Context, service string) (string, error) {
	if !s.systemctlEnabled {
		return "", errors.New("systemctl tools are disabled")
	}

	allowed, err := s.resolveAllowedService(service)
	if err != nil {
		return "", err
	}

	return s.run(ctx,
		"systemctl",
		"show",
		"--no-pager",
		"--property=Id,Description,LoadState,ActiveState,SubState,UnitFileState,MainPID,ExecMainStatus,ExecMainStartTimestamp",
		allowed,
	)
}

func (s *ServerControl) SystemctlAction(ctx context.Context, action string, service string) (string, error) {
	if !s.systemctlEnabled {
		return "", errors.New("systemctl tools are disabled")
	}

	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "start", "stop", "restart":
	default:
		return "", fmt.Errorf("unsupported systemctl action %q", action)
	}

	allowed, err := s.resolveAllowedService(service)
	if err != nil {
		return "", err
	}

	if _, err := s.run(ctx, "systemctl", action, allowed); err != nil {
		return "", err
	}

	status, err := s.SystemctlStatus(ctx, allowed)
	if err != nil {
		return action + " completed", nil
	}
	return status, nil
}

func (s *ServerControl) DockerPS(ctx context.Context) (string, error) {
	if !s.dockerEnabled {
		return "", errors.New("docker tools are disabled")
	}

	output, err := s.run(ctx, "docker", "ps", "-a", "--format", "{{.Names}}\t{{.Status}}\t{{.Image}}")
	if err != nil {
		return "", err
	}

	if len(s.allowedContainers) == 0 {
		return output, nil
	}

	lines := strings.Split(output, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) == 0 {
			continue
		}
		if _, ok := s.allowedContainers[normalizeLookup(fields[0])]; ok {
			filtered = append(filtered, line)
		}
	}

	if len(filtered) == 0 {
		return "No allowed containers found.", nil
	}
	return strings.Join(filtered, "\n"), nil
}

func (s *ServerControl) DockerLogs(ctx context.Context, container string, lines int) (string, error) {
	if !s.dockerEnabled {
		return "", errors.New("docker tools are disabled")
	}

	allowed, err := s.resolveAllowedContainer(container)
	if err != nil {
		return "", err
	}

	return s.run(ctx, "docker", "logs", "--tail", strconv.Itoa(s.clampLines(lines)), allowed)
}

func (s *ServerControl) JournalTail(ctx context.Context, unit string, lines int) (string, error) {
	if !s.journalEnabled {
		return "", errors.New("journalctl tools are disabled")
	}

	allowed, err := s.resolveAllowedUnit(unit)
	if err != nil {
		return "", err
	}

	return s.run(ctx, "journalctl", "-u", allowed, "-n", strconv.Itoa(s.clampLines(lines)), "--no-pager")
}

func (s *ServerControl) clampLines(lines int) int {
	if lines <= 0 {
		return s.defaultLogLines
	}
	if lines > s.maxLogLines {
		return s.maxLogLines
	}
	return lines
}

func (s *ServerControl) resolveAllowedService(service string) (string, error) {
	if len(s.allowedServices) == 0 {
		return "", errors.New("no systemctl services are allow-listed")
	}

	value, ok := s.allowedServices[normalizeLookup(service)]
	if !ok {
		return "", fmt.Errorf("service %q is not allow-listed", strings.TrimSpace(service))
	}
	return value, nil
}

func (s *ServerControl) resolveAllowedContainer(container string) (string, error) {
	if len(s.allowedContainers) == 0 {
		return "", errors.New("no docker containers are allow-listed")
	}

	value, ok := s.allowedContainers[normalizeLookup(container)]
	if !ok {
		return "", fmt.Errorf("container %q is not allow-listed", strings.TrimSpace(container))
	}
	return value, nil
}

func (s *ServerControl) resolveAllowedUnit(unit string) (string, error) {
	if len(s.allowedUnits) == 0 {
		return "", errors.New("no journal units are allow-listed")
	}

	value, ok := s.allowedUnits[normalizeLookup(unit)]
	if !ok {
		return "", fmt.Errorf("unit %q is not allow-listed", strings.TrimSpace(unit))
	}
	return value, nil
}

func (s *ServerControl) run(ctx context.Context, name string, args ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, name, args...)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if len(text) > maxCommandOutputChars {
		text = text[:maxCommandOutputChars] + "..."
	}

	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%s timed out after %s", name, s.timeout)
	}
	if err != nil {
		if text == "" {
			return "", fmt.Errorf("%s failed: %w", name, err)
		}
		return "", fmt.Errorf("%s failed: %s", name, text)
	}
	if text == "" {
		return "ok", nil
	}
	return text, nil
}

func normalizeCommandMap(source map[string]string) map[string]string {
	normalized := make(map[string]string, len(source))
	for name, command := range source {
		key := normalizeLookup(name)
		command = strings.TrimSpace(command)
		if key == "" || command == "" {
			continue
		}
		normalized[key] = command
	}
	return normalized
}

func normalizeNameSet(source []string, serviceAliases bool) map[string]string {
	normalized := make(map[string]string, len(source))
	for _, item := range source {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}

		key := normalizeLookup(value)
		normalized[key] = value
		if serviceAliases && strings.HasSuffix(key, ".service") {
			normalized[strings.TrimSuffix(key, ".service")] = value
		}
	}
	return normalized
}

func normalizeLookup(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedValues(values map[string]string) []string {
	seen := make(map[string]struct{}, len(values))
	items := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}
