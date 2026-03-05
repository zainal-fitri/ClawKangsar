package core

import "context"

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type LLMMessage struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
}

type LLMResponse struct {
	Content   string
	ToolCalls []ToolCall
}

type ChatProvider interface {
	Complete(ctx context.Context, messages []LLMMessage, tools []ToolDefinition) (LLMResponse, error)
}
