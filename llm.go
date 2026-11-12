package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func jsonMarshal(v any) ([]byte, error)   { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type LLM struct {
	cfg    *Config
	tools  *Tools
	client *http.Client
}

func NewLLM(cfg *Config, tools *Tools) *LLM {
	return &LLM{cfg: cfg, tools: tools, client: &http.Client{Timeout: cfg.LLMTimeout}}
}

func (l *LLM) complete(ctx context.Context, msgs []Message) (*Message, error) {
	body := map[string]any{"model": l.cfg.LLMModel, "messages": msgs}
	if defs := l.tools.Defs(); len(defs) > 0 {
		body["tools"] = defs
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(l.cfg.LLMBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+l.cfg.LLMAPIKey)
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("llm status %d: %s", resp.StatusCode, truncate(string(data), 500))
	}
	var out struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("llm returned no choices")
	}
	m := out.Choices[0].Message
	return &m, nil
}

// Run sends history (already including the new user message) to the model and
// executes tool calls until a final answer is produced.
func (l *LLM) Run(ctx context.Context, history []Message) (string, error) {
	msgs := append([]Message{{Role: "system", Content: l.cfg.SystemPrompt}}, history...)
	for round := 0; round <= l.cfg.MaxToolRounds; round++ {
		m, err := l.complete(ctx, msgs)
		if err != nil {
			return "", err
		}
		if len(m.ToolCalls) == 0 {
			return strings.TrimSpace(m.Content), nil
		}
		m.Role = "assistant"
		msgs = append(msgs, *m)
		for _, tc := range m.ToolCalls {
			var args map[string]any
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					msgs = append(msgs, Message{Role: "tool", ToolCallID: tc.ID, Content: "error: invalid arguments JSON"})
					continue
				}
			}
			res := l.tools.Call(ctx, tc.Function.Name, args)
			msgs = append(msgs, Message{Role: "tool", ToolCallID: tc.ID, Content: res})
		}
	}
	return "", fmt.Errorf("tool call limit (%d rounds) reached", l.cfg.MaxToolRounds)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
