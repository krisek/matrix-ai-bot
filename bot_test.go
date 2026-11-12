package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in   string
		rest string
		ok   bool
	}{
		{"@ai hello", "hello", true},
		{"@AI: hello", "hello", true},
		{"  @ai /reset ", "/reset", true},
		{"@ai", "", true},
		{"@aiming high", "", false},
		{"hey @ai hello", "", false},
	}
	for _, c := range cases {
		rest, ok := parseCommand(c.in, "@ai")
		if rest != c.rest || ok != c.ok {
			t.Errorf("parseCommand(%q) = %q,%v want %q,%v", c.in, rest, ok, c.rest, c.ok)
		}
	}
}

type echoArgs struct {
	Text string `json:"text"`
}

func newMCPServer(t *testing.T, wantAuth string) *httptest.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "echo", Description: "echo text"},
		func(ctx context.Context, r *mcp.CallToolRequest, a echoArgs) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "echo:" + a.Text}}}, nil, nil
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "peek", Description: "read only", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(ctx context.Context, r *mcp.CallToolRequest, a echoArgs) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "peeked"}}}, nil, nil
		})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/secret/mcp" {
			http.NotFound(w, r)
			return
		}
		if wantAuth != "" && r.Header.Get("Authorization") != wantAuth {
			http.Error(w, "unauthorized", 401)
			return
		}
		h.ServeHTTP(w, r)
	}))
}

// fakeLLM answers: "use tool" -> tool call echo; after tool result -> final text.
func fakeLLM(t *testing.T, seen *[][]Message, mu *sync.Mutex) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" || r.URL.Path != "/chat/completions" {
			http.Error(w, "bad", 400)
			return
		}
		var req struct {
			Messages []Message        `json:"messages"`
			Tools    []map[string]any `json:"tools"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		*seen = append(*seen, req.Messages)
		mu.Unlock()
		last := req.Messages[len(req.Messages)-1]
		var msg map[string]any
		if last.Role == "tool" {
			msg = map[string]any{"role": "assistant", "content": "result: " + last.Content}
		} else if strings.Contains(last.Content, "use tool") && len(req.Tools) > 0 {
			msg = map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{
				"id": "c1", "type": "function",
				"function": map[string]any{"name": "fake_echo", "arguments": `{"text":"hi"}`},
			}}}
		} else {
			msg = map[string]any{"role": "assistant", "content": "plain"}
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg}}})
	}))
}
