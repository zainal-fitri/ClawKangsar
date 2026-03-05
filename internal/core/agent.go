package core

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type BrowserTool interface {
	Browse(ctx context.Context, rawURL string) (string, error)
}

type WebFetchTool interface {
	Fetch(ctx context.Context, rawURL string) (string, error)
}

type ServerTool interface {
	ShellAvailable() bool
	SystemctlAvailable() bool
	DockerAvailable() bool
	JournalAvailable() bool
	ShellCommandNames() []string
	AllowedServices() []string
	AllowedContainers() []string
	AllowedUnits() []string
	RunNamedCommand(ctx context.Context, name string) (string, error)
	SystemctlStatus(ctx context.Context, service string) (string, error)
	SystemctlAction(ctx context.Context, action string, service string) (string, error)
	DockerPS(ctx context.Context) (string, error)
	DockerLogs(ctx context.Context, container string, lines int) (string, error)
	JournalTail(ctx context.Context, unit string, lines int) (string, error)
}

type AgentStats struct {
	InMemoryMessages int `json:"in_memory_messages"`
	StoredSessions   int `json:"stored_sessions"`
	StoredMessages   int `json:"stored_messages"`
}

type Agent struct {
	mu           sync.Mutex
	systemPrompt string
	browser      BrowserTool
	webFetch     WebFetchTool
	server       ServerTool
	llm          ChatProvider
	sessions     *SessionStore
	memory       []Message
	maxMemory    int
}

func NewAgent(systemPrompt string, browser BrowserTool, webFetch WebFetchTool, server ServerTool, llm ChatProvider, sessions *SessionStore) *Agent {
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = "You are ClawKangsar, a professional assistant running on a Raspberry Pi. Keep responses concise and use your browser tool only when real-time data is needed."
	}

	return &Agent{
		systemPrompt: systemPrompt,
		browser:      browser,
		webFetch:     webFetch,
		server:       server,
		llm:          llm,
		sessions:     sessions,
		memory:       make([]Message, 0, 64),
		maxMemory:    128,
	}
}

func (a *Agent) Process(ctx context.Context, msg Message) (string, error) {
	msg.Text = strings.TrimSpace(msg.Text)
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	if msg.Text == "" {
		return "", nil
	}

	sessionKey := messageSessionKey(msg)
	memorySize := a.remember(msg)
	lower := strings.ToLower(msg.Text)

	if strings.HasPrefix(lower, "/status") {
		stats := a.Stats()
		return fmt.Sprintf("ClawKangsar status: memory=%d sessions=%d stored_messages=%d",
			stats.InMemoryMessages,
			stats.StoredSessions,
			stats.StoredMessages,
		), nil
	}

	if strings.HasPrefix(lower, "/fetch ") && a.webFetch != nil {
		target := strings.TrimSpace(msg.Text[len("/fetch "):])
		if target == "" {
			return "Provide a URL after /fetch.", nil
		}
		text, err := a.webFetch.Fetch(ctx, target)
		if err != nil {
			return "", err
		}
		return truncate(text, 1200), nil
	}

	if strings.HasPrefix(lower, "/browse ") {
		target := strings.TrimSpace(msg.Text[len("/browse "):])
		if target == "" {
			return "Provide a URL after /browse.", nil
		}

		if a.webFetch != nil {
			// Try lightweight HTTP fetch first to avoid booting Chromium on Pi.
			text, err := a.webFetch.Fetch(ctx, target)
			if err == nil && strings.TrimSpace(text) != "" {
				return truncate(text, 1200), nil
			}
		}

		if a.browser == nil {
			return "Browser tool unavailable.", nil
		}

		text, err := a.browser.Browse(ctx, target)
		if err != nil {
			return "", fmt.Errorf("browse failed: %w", err)
		}
		return truncate(text, 1200), nil
	}

	if strings.HasPrefix(lower, "/cmd ") && a.server != nil {
		name := strings.TrimSpace(msg.Text[len("/cmd "):])
		if name == "" {
			return "Usage: /cmd <name>.", nil
		}
		text, err := a.server.RunNamedCommand(ctx, name)
		if err != nil {
			return "", err
		}
		return truncate(text, 2000), nil
	}

	if strings.HasPrefix(lower, "/service ") && a.server != nil {
		text, err := a.handleServiceCommand(ctx, msg.Text)
		if err != nil {
			return "", err
		}
		return truncate(text, 2000), nil
	}

	if strings.HasPrefix(lower, "/docker ") && a.server != nil {
		text, err := a.handleDockerCommand(ctx, msg.Text)
		if err != nil {
			return "", err
		}
		return truncate(text, 2000), nil
	}

	if strings.HasPrefix(lower, "/logs ") && a.server != nil {
		text, err := a.handleLogsCommand(ctx, msg.Text)
		if err != nil {
			return "", err
		}
		return truncate(text, 2000), nil
	}

	if a.llm != nil {
		reply, err := a.replyWithLLM(ctx, sessionKey)
		if err != nil {
			return "", err
		}
		a.rememberAssistant(msg, sessionKey, reply)
		return truncate(reply, 2000), nil
	}

	return fmt.Sprintf("ClawKangsar ready. Channel=%s memory=%d. Configure llm.enabled=true for real replies.", msg.Channel, memorySize), nil
}

func (a *Agent) remember(msg Message) int {
	a.mu.Lock()

	a.memory = append(a.memory, msg)
	if len(a.memory) > a.maxMemory {
		a.memory = a.memory[len(a.memory)-a.maxMemory:]
	}
	currentSize := len(a.memory)
	a.mu.Unlock()

	if a.sessions != nil {
		sessionKey := messageSessionKey(msg)
		_ = a.sessions.AddMessage(sessionKey, msg)
		if sessionKey != "global" {
			_ = a.sessions.AddMessage("global", msg)
		}
	}

	return currentSize
}

func (a *Agent) rememberAssistant(source Message, sessionKey string, reply string) {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return
	}

	assistantMsg := Message{
		Channel:   "assistant",
		UserID:    source.UserID,
		ChatID:    source.ChatID,
		Text:      reply,
		Timestamp: time.Now(),
	}

	a.mu.Lock()
	a.memory = append(a.memory, assistantMsg)
	if len(a.memory) > a.maxMemory {
		a.memory = a.memory[len(a.memory)-a.maxMemory:]
	}
	a.mu.Unlock()

	if a.sessions != nil {
		_ = a.sessions.AddMessage(sessionKey, assistantMsg)
		if sessionKey != "global" {
			_ = a.sessions.AddMessage("global", assistantMsg)
		}
	}
}

func (a *Agent) replyWithLLM(ctx context.Context, sessionKey string) (string, error) {
	if a.llm == nil {
		return "", fmt.Errorf("llm provider not configured")
	}

	messages := a.buildLLMMessages(sessionKey)
	tools := a.availableTools()

	for i := 0; i < 4; i++ {
		response, err := a.llm.Complete(ctx, messages, tools)
		if err != nil {
			return "", err
		}
		if len(response.ToolCalls) == 0 {
			return strings.TrimSpace(response.Content), nil
		}

		messages = append(messages, LLMMessage{
			Role:      "assistant",
			Content:   strings.TrimSpace(response.Content),
			ToolCalls: response.ToolCalls,
		})

		for _, call := range response.ToolCalls {
			output := a.executeToolCall(ctx, call)
			messages = append(messages, LLMMessage{
				Role:       "tool",
				Content:    output,
				ToolCallID: call.ID,
			})
		}
	}

	return "", fmt.Errorf("llm exceeded tool-call iteration limit")
}

func (a *Agent) buildLLMMessages(sessionKey string) []LLMMessage {
	messages := make([]LLMMessage, 0, 17)
	if strings.TrimSpace(a.systemPrompt) != "" {
		messages = append(messages, LLMMessage{
			Role:    "system",
			Content: a.systemPrompt,
		})
	}

	var history []Message
	if a.sessions != nil {
		history = a.sessions.History(sessionKey)
	} else {
		a.mu.Lock()
		history = make([]Message, len(a.memory))
		copy(history, a.memory)
		a.mu.Unlock()
	}

	for _, item := range history {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}

		role := "user"
		switch item.Channel {
		case "assistant":
			role = "assistant"
		case "system":
			role = "system"
		case "tool":
			role = "tool"
		}

		messages = append(messages, LLMMessage{
			Role:    role,
			Content: text,
		})
	}

	return messages
}

func (a *Agent) availableTools() []ToolDefinition {
	tools := make([]ToolDefinition, 0, 8)
	if a.webFetch != nil {
		tools = append(tools, ToolDefinition{
			Name:        "web_fetch",
			Description: "Fetch a URL using a lightweight HTTP request and return readable text content. Prefer this for real-time data.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "HTTP or HTTPS URL to fetch",
					},
				},
				"required": []string{"url"},
			},
		})
	}
	if a.browser != nil {
		tools = append(tools, ToolDefinition{
			Name:        "browser_browse",
			Description: "Open a URL in the headless browser and return visible page text. Use only when lightweight fetch is insufficient.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "HTTP or HTTPS URL to browse",
					},
				},
				"required": []string{"url"},
			},
		})
	}
	if a.server != nil {
		if a.server.ShellAvailable() {
			commands := a.server.ShellCommandNames()
			tools = append(tools, ToolDefinition{
				Name:        "shell_command",
				Description: "Run one pre-approved named shell command alias. Allowed names: " + strings.Join(commands, ", "),
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "Approved command alias to run",
						},
					},
					"required": []string{"name"},
				},
			})
		}
		if a.server.SystemctlAvailable() {
			services := a.server.AllowedServices()
			tools = append(tools, ToolDefinition{
				Name:        "systemctl_status",
				Description: "Get concise systemd service status for one allow-listed service. Allowed services: " + strings.Join(services, ", "),
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"service": map[string]any{
							"type":        "string",
							"description": "Allow-listed systemd service name",
						},
					},
					"required": []string{"service"},
				},
			})
			tools = append(tools, ToolDefinition{
				Name:        "systemctl_action",
				Description: "Run start, stop, or restart on one allow-listed service, then return the updated status.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"service": map[string]any{
							"type":        "string",
							"description": "Allow-listed systemd service name",
						},
						"action": map[string]any{
							"type":        "string",
							"description": "One of: start, stop, restart",
						},
					},
					"required": []string{"service", "action"},
				},
			})
		}
		if a.server.DockerAvailable() {
			tools = append(tools, ToolDefinition{
				Name:        "docker_ps",
				Description: "List Docker containers and their status if Docker tools are enabled.",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			})
		}
		if containers := a.server.AllowedContainers(); len(containers) > 0 {
			tools = append(tools, ToolDefinition{
				Name:        "docker_logs",
				Description: "Read recent logs for one allow-listed Docker container. Allowed containers: " + strings.Join(containers, ", "),
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"container": map[string]any{
							"type":        "string",
							"description": "Allow-listed Docker container name",
						},
						"lines": map[string]any{
							"type":        "integer",
							"description": "Optional number of log lines to return",
						},
					},
					"required": []string{"container"},
				},
			})
		}
		if a.server.JournalAvailable() {
			units := a.server.AllowedUnits()
			tools = append(tools, ToolDefinition{
				Name:        "journal_tail",
				Description: "Read recent journal logs for one allow-listed systemd unit. Allowed units: " + strings.Join(units, ", "),
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"unit": map[string]any{
							"type":        "string",
							"description": "Allow-listed systemd unit name",
						},
						"lines": map[string]any{
							"type":        "integer",
							"description": "Optional number of log lines to return",
						},
					},
					"required": []string{"unit"},
				},
			})
		}
	}
	return tools
}

func (a *Agent) executeToolCall(ctx context.Context, call ToolCall) string {
	switch call.Name {
	case "web_fetch":
		url := getStringArgument(call.Arguments, "url")
		if url == "" {
			return "tool error: missing required string field `url`"
		}
		if a.webFetch == nil {
			return "tool error: web_fetch is unavailable"
		}
		text, err := a.webFetch.Fetch(ctx, url)
		if err != nil {
			return "tool error: " + err.Error()
		}
		return truncate(text, 4000)
	case "browser_browse":
		url := getStringArgument(call.Arguments, "url")
		if url == "" {
			return "tool error: missing required string field `url`"
		}
		if a.browser == nil {
			return "tool error: browser_browse is unavailable"
		}
		text, err := a.browser.Browse(ctx, url)
		if err != nil {
			return "tool error: " + err.Error()
		}
		return truncate(text, 4000)
	case "shell_command":
		if a.server == nil {
			return "tool error: shell_command is unavailable"
		}
		name := getStringArgument(call.Arguments, "name")
		if name == "" {
			return "tool error: missing required string field `name`"
		}
		text, err := a.server.RunNamedCommand(ctx, name)
		if err != nil {
			return "tool error: " + err.Error()
		}
		return truncate(text, 4000)
	case "systemctl_status":
		if a.server == nil {
			return "tool error: systemctl_status is unavailable"
		}
		service := getStringArgument(call.Arguments, "service")
		if service == "" {
			return "tool error: missing required string field `service`"
		}
		text, err := a.server.SystemctlStatus(ctx, service)
		if err != nil {
			return "tool error: " + err.Error()
		}
		return truncate(text, 4000)
	case "systemctl_action":
		if a.server == nil {
			return "tool error: systemctl_action is unavailable"
		}
		service := getStringArgument(call.Arguments, "service")
		action := getStringArgument(call.Arguments, "action")
		if service == "" {
			return "tool error: missing required string field `service`"
		}
		if action == "" {
			return "tool error: missing required string field `action`"
		}
		text, err := a.server.SystemctlAction(ctx, action, service)
		if err != nil {
			return "tool error: " + err.Error()
		}
		return truncate(text, 4000)
	case "docker_ps":
		if a.server == nil {
			return "tool error: docker_ps is unavailable"
		}
		text, err := a.server.DockerPS(ctx)
		if err != nil {
			return "tool error: " + err.Error()
		}
		return truncate(text, 4000)
	case "docker_logs":
		if a.server == nil {
			return "tool error: docker_logs is unavailable"
		}
		container := getStringArgument(call.Arguments, "container")
		if container == "" {
			return "tool error: missing required string field `container`"
		}
		text, err := a.server.DockerLogs(ctx, container, getIntArgument(call.Arguments, "lines"))
		if err != nil {
			return "tool error: " + err.Error()
		}
		return truncate(text, 4000)
	case "journal_tail":
		if a.server == nil {
			return "tool error: journal_tail is unavailable"
		}
		unit := getStringArgument(call.Arguments, "unit")
		if unit == "" {
			return "tool error: missing required string field `unit`"
		}
		text, err := a.server.JournalTail(ctx, unit, getIntArgument(call.Arguments, "lines"))
		if err != nil {
			return "tool error: " + err.Error()
		}
		return truncate(text, 4000)
	default:
		return "tool error: unknown tool `" + call.Name + "`"
	}
}

func (a *Agent) Stats() AgentStats {
	a.mu.Lock()
	inMemory := len(a.memory)
	a.mu.Unlock()

	stats := AgentStats{
		InMemoryMessages: inMemory,
	}
	if a.sessions != nil {
		stats.StoredSessions = a.sessions.SessionCount()
		stats.StoredMessages = a.sessions.TotalMessageCount()
	}
	return stats
}

func messageSessionKey(msg Message) string {
	if strings.TrimSpace(msg.Channel) != "" && strings.TrimSpace(msg.ChatID) != "" {
		return msg.Channel + ":" + msg.ChatID
	}
	if strings.TrimSpace(msg.UserID) != "" {
		return "user:" + strings.TrimSpace(msg.UserID)
	}
	return "global"
}

func truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max] + "..."
}

func (a *Agent) handleServiceCommand(ctx context.Context, text string) (string, error) {
	fields := strings.Fields(text)
	if len(fields) < 3 {
		return "Usage: /service status <name> or /service <start|stop|restart> <name>.", nil
	}

	action := strings.ToLower(strings.TrimSpace(fields[1]))
	service := strings.TrimSpace(fields[2])

	switch action {
	case "status":
		return a.server.SystemctlStatus(ctx, service)
	case "start", "stop", "restart":
		return a.server.SystemctlAction(ctx, action, service)
	default:
		return "Usage: /service status <name> or /service <start|stop|restart> <name>.", nil
	}
}

func (a *Agent) handleDockerCommand(ctx context.Context, text string) (string, error) {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return "Usage: /docker ps or /docker logs <container> [lines].", nil
	}

	action := strings.ToLower(strings.TrimSpace(fields[1]))
	switch action {
	case "ps":
		return a.server.DockerPS(ctx)
	case "logs":
		if len(fields) < 3 {
			return "Usage: /docker logs <container> [lines].", nil
		}
		lines := 0
		if len(fields) >= 4 {
			lines = parseOptionalInt(fields[3])
		}
		return a.server.DockerLogs(ctx, fields[2], lines)
	default:
		return "Usage: /docker ps or /docker logs <container> [lines].", nil
	}
}

func (a *Agent) handleLogsCommand(ctx context.Context, text string) (string, error) {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return "Usage: /logs <unit> [lines].", nil
	}

	lines := 0
	if len(fields) >= 3 {
		lines = parseOptionalInt(fields[2])
	}
	return a.server.JournalTail(ctx, fields[1], lines)
}

func getStringArgument(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func getIntArgument(values map[string]any, key string) int {
	raw, ok := values[key]
	if !ok {
		return 0
	}

	switch value := raw.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		return parseOptionalInt(value)
	default:
		return 0
	}
}

func parseOptionalInt(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}
