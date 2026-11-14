package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func lastLen(seen *[][]Message, mu *sync.Mutex) int {
	mu.Lock()
	defer mu.Unlock()
	return len((*seen)[len(*seen)-1])
}

func TestEndToEnd(t *testing.T) {
	mcpSrv := newMCPServer(t, "Bearer tok")
	defer mcpSrv.Close()
	t.Setenv("FAKE_URL", mcpSrv.URL+"/secret/mcp")
	t.Setenv("FAKE_TOKEN", "tok")

	var seen [][]Message
	var mu sync.Mutex
	llmSrv := fakeLLM(t, &seen, &mu)
	defer llmSrv.Close()

	cfg := &Config{
		Callword: "@ai", LLMBaseURL: llmSrv.URL, LLMModel: "m", LLMAPIKey: "k",
		SystemPrompt: "sys", MaxHistory: 10, MaxToolRounds: 3, MaxToolResultChars: 100,
		LLMTimeout: 5 * time.Second,
		MCPServers: []MCPServer{{Name: "fake", URLEnv: "FAKE_URL", BearerEnv: "FAKE_TOKEN"}},
	}
	ctx := context.Background()
	tools := ConnectTools(ctx, cfg.MCPServers, cfg.MaxToolResultChars)
	defer tools.Close()
	if len(tools.Defs()) != 2 {
		t.Fatalf("want 2 tools, got %d", len(tools.Defs()))
	}
	chat := NewChat(cfg, NewLLM(cfg, tools))

	if got := chat.Handle(ctx, "!r", "not for me"); got != "" {
		t.Fatalf("unexpected reply %q", got)
	}
	if got := chat.Handle(ctx, "!r", "@ai use tool"); got != "result: echo:hi" {
		t.Fatalf("tool flow got %q", got)
	}
	if got := chat.Handle(ctx, "!r", "@ai hello"); got != "plain" {
		t.Fatalf("got %q", got)
	}
	// system + user + assistant + user("hello") = 4 (tool turns are not stored)
	if n := lastLen(&seen, &mu); n != 4 {
		t.Fatalf("history not sent, got %d messages", n)
	}
	chat.Handle(ctx, "!other", "@ai hello")
	if n := lastLen(&seen, &mu); n != 2 {
		t.Fatalf("other room should have 2 messages, got %d", n)
	}
	if got := chat.Handle(ctx, "!r", "@ai /reset"); got != "Conversation reset." {
		t.Fatalf("reset got %q", got)
	}
	chat.Handle(ctx, "!r", "@ai again")
	if n := lastLen(&seen, &mu); n != 2 {
		t.Fatalf("after reset want 2 messages, got %d", n)
	}
}

func TestReadOnlyAndAllowlist(t *testing.T) {
	srv := newMCPServer(t, "")
	defer srv.Close()
	t.Setenv("FAKE_URL", srv.URL+"/secret/mcp")
	ro := ConnectTools(context.Background(), []MCPServer{{Name: "f", URLEnv: "FAKE_URL", ReadOnly: true}}, 100)
	defer ro.Close()
	if len(ro.Defs()) != 1 || ro.Defs()[0]["function"].(map[string]any)["name"] != "f_peek" {
		t.Fatalf("readOnly filter failed: %v", ro.Defs())
	}
	al := ConnectTools(context.Background(), []MCPServer{{Name: "f", URLEnv: "FAKE_URL", AllowedTools: []string{"echo"}}}, 100)
	defer al.Close()
	if len(al.Defs()) != 1 || al.Defs()[0]["function"].(map[string]any)["name"] != "f_echo" {
		t.Fatalf("allowlist failed: %v", al.Defs())
	}
	if out := al.Call(context.Background(), "f_echo", map[string]any{"text": strings.Repeat("x", 500)}); !strings.HasSuffix(out, "[truncated]") {
		t.Fatalf("expected truncation, got %q", out)
	}
}

func TestSanitizeSchema(t *testing.T) {
	out := sanitizeSchema(map[string]any{
		"$schema": "x", "additionalProperties": false,
		"properties": map[string]any{"a": map[string]any{"type": "string", "additionalProperties": false}},
	})
	if _, ok := out["$schema"]; ok {
		t.Fatal("$schema kept")
	}
	if out["type"] != "object" {
		t.Fatal("type missing")
	}
	if _, ok := out["properties"].(map[string]any)["a"].(map[string]any)["additionalProperties"]; ok {
		t.Fatal("nested additionalProperties kept")
	}
	if e := sanitizeSchema(nil); e["type"] != "object" || e["properties"] == nil {
		t.Fatalf("empty schema: %v", e)
	}
}

func TestListenMode(t *testing.T) {
	var seen [][]Message
	var mu sync.Mutex
	llmSrv := fakeLLM(t, &seen, &mu)
	defer llmSrv.Close()
	cfg := &Config{Callword: "@ai", LLMBaseURL: llmSrv.URL, LLMModel: "m", LLMAPIKey: "k",
		SystemPrompt: "sys", MaxHistory: 10, MaxToolRounds: 3, LLMTimeout: 5 * time.Second}
	chat := NewChat(cfg, NewLLM(cfg, &Tools{route: map[string]route{}}))
	ctx := context.Background()

	if got := chat.Handle(ctx, "!r", "plain message"); got != "" {
		t.Fatalf("must ignore before /listen, got %q", got)
	}
	if got := chat.Handle(ctx, "!r", "@ai /listen"); !strings.HasPrefix(got, "Listening") {
		t.Fatalf("listen got %q", got)
	}
	if got := chat.Handle(ctx, "!r", "no callword needed"); got != "plain" {
		t.Fatalf("listening should answer, got %q", got)
	}
	// other rooms are unaffected
	if got := chat.Handle(ctx, "!other", "hello"); got != "" {
		t.Fatalf("other room must stay silent, got %q", got)
	}
	// a bare /silence without callword is just a prompt, not a command
	if got := chat.Handle(ctx, "!r", "/silence"); got != "plain" {
		t.Fatalf("bare /silence must be a prompt, got %q", got)
	}
	if got := chat.Handle(ctx, "!r", "@ai /silence"); !strings.HasPrefix(got, "Silenced") {
		t.Fatalf("silence got %q", got)
	}
	if got := chat.Handle(ctx, "!r", "ignored again"); got != "" {
		t.Fatalf("must ignore after /silence, got %q", got)
	}
	if got := chat.Handle(ctx, "!r", "@ai still works"); got != "plain" {
		t.Fatalf("callword must still work, got %q", got)
	}
}
