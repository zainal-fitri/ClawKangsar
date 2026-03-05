package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"clawkangsar/internal/config"
	"clawkangsar/internal/core"
)

type OpenAICompatProvider struct {
	baseURL     string
	apiKey      string
	model       string
	temperature float64
	maxTokens   int
	timeout     time.Duration
	client      *http.Client
}

type openAICompatRequest struct {
	Model       string                    `json:"model"`
	Messages    []openAICompatMessage     `json:"messages"`
	Tools       []openAICompatTool        `json:"tools,omitempty"`
	ToolChoice  string                    `json:"tool_choice,omitempty"`
	Temperature float64                   `json:"temperature,omitempty"`
	MaxTokens   int                       `json:"max_tokens,omitempty"`
}

type openAICompatMessage struct {
	Role       string                   `json:"role"`
	Content    any                      `json:"content,omitempty"`
	ToolCalls  []openAICompatToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string                   `json:"tool_call_id,omitempty"`
}

type openAICompatTool struct {
	Type     string                   `json:"type"`
	Function openAICompatToolFunction `json:"function"`
}

type openAICompatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAICompatToolCall struct {
	ID       string                      `json:"id"`
	Type     string                      `json:"type"`
	Function openAICompatToolCallPayload `json:"function"`
}

type openAICompatToolCallPayload struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAICompatResponse struct {
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
	Choices []struct {
		Message struct {
			Content   any                    `json:"content"`
			ToolCalls []openAICompatToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

func NewOpenAICompatProvider(cfg config.LLMConfig) (*OpenAICompatProvider, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("llm.model is required when llm.enabled=true")
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return nil, errors.New("llm.base_url is required when llm.enabled=true")
	}

	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" && strings.TrimSpace(cfg.APIKeyEnv) != "" {
		apiKey = strings.TrimSpace(os.Getenv(cfg.APIKeyEnv))
	}
	if apiKey == "" {
		return nil, errors.New("llm.api_key or llm.api_key_env is required for openai_compat")
	}

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	return &OpenAICompatProvider{
		baseURL:     strings.TrimRight(baseURL, "/"),
		apiKey:      apiKey,
		model:       strings.TrimSpace(cfg.Model),
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
		timeout:     timeout,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func (p *OpenAICompatProvider) Complete(
	ctx context.Context,
	messages []core.LLMMessage,
	tools []core.ToolDefinition,
) (core.LLMResponse, error) {
	reqBody := openAICompatRequest{
		Model:       p.model,
		Messages:    translateOpenAIMessages(messages),
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
	}
	if len(tools) > 0 {
		reqBody.Tools = translateOpenAITools(tools)
		reqBody.ToolChoice = "auto"
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return core.LLMResponse{}, fmt.Errorf("marshal llm request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return core.LLMResponse{}, fmt.Errorf("build llm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return core.LLMResponse{}, fmt.Errorf("send llm request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return core.LLMResponse{}, fmt.Errorf("read llm response: %w", err)
	}

	var decoded openAICompatResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return core.LLMResponse{}, fmt.Errorf("parse llm response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if decoded.Error != nil && decoded.Error.Message != "" {
			return core.LLMResponse{}, fmt.Errorf("llm request failed: %s", decoded.Error.Message)
		}
		return core.LLMResponse{}, fmt.Errorf("llm request failed with status %d", resp.StatusCode)
	}
	if decoded.Error != nil && decoded.Error.Message != "" {
		return core.LLMResponse{}, fmt.Errorf("llm error: %s", decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 {
		return core.LLMResponse{}, errors.New("llm returned no choices")
	}

	choice := decoded.Choices[0].Message
	result := core.LLMResponse{
		Content: strings.TrimSpace(extractContent(choice.Content)),
	}
	for _, toolCall := range choice.ToolCalls {
		args := make(map[string]any)
		if strings.TrimSpace(toolCall.Function.Arguments) != "" {
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				args["raw"] = toolCall.Function.Arguments
			}
		}
		result.ToolCalls = append(result.ToolCalls, core.ToolCall{
			ID:        toolCall.ID,
			Name:      toolCall.Function.Name,
			Arguments: args,
		})
	}

	return result, nil
}

func translateOpenAIMessages(messages []core.LLMMessage) []openAICompatMessage {
	out := make([]openAICompatMessage, 0, len(messages))
	for _, item := range messages {
		msg := openAICompatMessage{
			Role:    item.Role,
			Content: item.Content,
		}
		if item.Role == "tool" {
			msg.ToolCallID = item.ToolCallID
		}
		if len(item.ToolCalls) > 0 {
			msg.ToolCalls = make([]openAICompatToolCall, 0, len(item.ToolCalls))
			for _, call := range item.ToolCalls {
				argsJSON, _ := json.Marshal(call.Arguments)
				msg.ToolCalls = append(msg.ToolCalls, openAICompatToolCall{
					ID:   call.ID,
					Type: "function",
					Function: openAICompatToolCallPayload{
						Name:      call.Name,
						Arguments: string(argsJSON),
					},
				})
			}
		}
		out = append(out, msg)
	}
	return out
}

func translateOpenAITools(tools []core.ToolDefinition) []openAICompatTool {
	out := make([]openAICompatTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, openAICompatTool{
			Type: "function",
			Function: openAICompatToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}
	return out
}

func extractContent(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}
