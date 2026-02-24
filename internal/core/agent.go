package core

import (
	"context"
	"fmt"
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
	sessions     *SessionStore
	memory       []Message
	maxMemory    int
}

func NewAgent(systemPrompt string, browser BrowserTool, webFetch WebFetchTool, sessions *SessionStore) *Agent {
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = "You are ClawKangsar, a professional assistant running on a Raspberry Pi. Keep responses concise and use your browser tool only when real-time data is needed."
	}

	return &Agent{
		systemPrompt: systemPrompt,
		browser:      browser,
		webFetch:     webFetch,
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

	return fmt.Sprintf("ClawKangsar ready. Channel=%s memory=%d", msg.Channel, memorySize), nil
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
