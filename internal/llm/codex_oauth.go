package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"clawkangsar/internal/auth"
	"clawkangsar/internal/config"
	"clawkangsar/internal/core"
)

const (
	defaultCodexBaseURL      = "https://chatgpt.com/backend-api/codex"
	defaultCodexInstructions = "You are ClawKangsar, a professional assistant running on a Raspberry Pi. Keep responses concise and use your browser tool only when real-time data is needed."
)

type CodexOAuthProvider struct {
	baseURL       string
	model         string
	authMethod    string
	codexAuthPath string
	timeout       time.Duration
	client        *http.Client
}

type codexResponseEvent struct {
	Type     string `json:"type"`
	Response struct {
		Status string `json:"status"`
		Output []struct {
			ID        string `json:"id"`
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	} `json:"response"`
}

func NewCodexOAuthProvider(cfg config.LLMConfig) (*CodexOAuthProvider, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("llm.model is required when llm.enabled=true")
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" || baseURL == "https://api.openai.com/v1" {
		baseURL = defaultCodexBaseURL
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	authMethod := strings.TrimSpace(cfg.AuthMethod)
	if authMethod == "" {
		authMethod = "codex_cli"
	}
	if authMethod != "codex_cli" {
		return nil, fmt.Errorf("unsupported codex oauth auth_method: %s", authMethod)
	}

	return &CodexOAuthProvider{
		baseURL:       strings.TrimRight(baseURL, "/"),
		model:         strings.TrimSpace(cfg.Model),
		authMethod:    authMethod,
		codexAuthPath: strings.TrimSpace(cfg.CodexAuthPath),
		timeout:       timeout,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func (p *CodexOAuthProvider) Complete(
	ctx context.Context,
	messages []core.LLMMessage,
	tools []core.ToolDefinition,
) (core.LLMResponse, error) {
	cred, err := auth.LoadCodexCredential(p.codexAuthPath)
	if err != nil {
		return core.LLMResponse{}, err
	}
	if strings.TrimSpace(cred.AccountID) == "" {
		return core.LLMResponse{}, errors.New("codex oauth credential is missing account_id; run `codex --login` again")
	}

	reqBody := p.buildRequest(messages, tools)
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return core.LLMResponse{}, fmt.Errorf("marshal codex request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.baseURL+"/responses", bytes.NewReader(payload))
	if err != nil {
		return core.LLMResponse{}, fmt.Errorf("build codex request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	req.Header.Set("Chatgpt-Account-Id", cred.AccountID)
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("OpenAI-Beta", "responses=experimental")

	resp, err := p.client.Do(req)
	if err != nil {
		return core.LLMResponse{}, fmt.Errorf("send codex request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return core.LLMResponse{}, fmt.Errorf("codex request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return parseCodexStream(resp.Body)
}

func (p *CodexOAuthProvider) buildRequest(messages []core.LLMMessage, tools []core.ToolDefinition) map[string]any {
	instructions := defaultCodexInstructions
	input := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			if strings.TrimSpace(msg.Content) != "" {
				instructions = msg.Content
			}
		case "user", "assistant":
			if len(msg.ToolCalls) > 0 {
				if strings.TrimSpace(msg.Content) != "" {
					input = append(input, map[string]any{
						"type": "message",
						"role": "assistant",
						"content": msg.Content,
					})
				}
				for _, call := range msg.ToolCalls {
					argsJSON, _ := json.Marshal(call.Arguments)
					input = append(input, map[string]any{
						"type":      "function_call",
						"call_id":   call.ID,
						"name":      call.Name,
						"arguments": string(argsJSON),
					})
				}
				continue
			}
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			input = append(input, map[string]any{
				"type": "message",
				"role": msg.Role,
				"content": []map[string]any{
					{
						"type": "input_text",
						"text": msg.Content,
					},
				},
			})
		case "tool":
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": msg.ToolCallID,
				"output":  msg.Content,
			})
		}
	}

	req := map[string]any{
		"model":        p.model,
		"instructions": instructions,
		"input":        input,
		"store":        false,
		"stream":       true,
	}
	if len(tools) > 0 {
		req["tools"] = translateCodexTools(tools)
	}
	return req
}

func translateCodexTools(tools []core.ToolDefinition) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{
			"type": "function",
			"name": tool.Name,
			"description": tool.Description,
			"parameters": tool.Parameters,
			"strict": false,
		})
	}
	return out
}

func parseCodexStream(body io.Reader) (core.LLMResponse, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			break
		}

		var event codexResponseEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}
		if event.Type != "response.completed" {
			continue
		}

		result := core.LLMResponse{}
		for _, item := range event.Response.Output {
			switch item.Type {
			case "message":
				for _, block := range item.Content {
					if block.Type == "output_text" && strings.TrimSpace(block.Text) != "" {
						if result.Content != "" {
							result.Content += "\n"
						}
						result.Content += block.Text
					}
				}
			case "function_call":
				args := make(map[string]any)
				if strings.TrimSpace(item.Arguments) != "" {
					if err := json.Unmarshal([]byte(item.Arguments), &args); err != nil {
						args["raw"] = item.Arguments
					}
				}
				result.ToolCalls = append(result.ToolCalls, core.ToolCall{
					ID:        item.CallID,
					Name:      item.Name,
					Arguments: args,
				})
			}
		}
		return result, nil
	}

	if err := scanner.Err(); err != nil {
		return core.LLMResponse{}, fmt.Errorf("read codex stream: %w", err)
	}
	return core.LLMResponse{}, errors.New("codex stream ended without completed response")
}
