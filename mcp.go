package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tools aggregates tools from several MCP servers and routes calls to them.
type Tools struct {
	defs    []map[string]any // OpenAI "tools" entries
	route   map[string]route
	maxChar int
}

type route struct {
	session *mcp.ClientSession
	tool    string
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

// ConnectTools connects to all configured servers. A failing server is logged
// and skipped so that the bot still works with the remaining ones.
func ConnectTools(ctx context.Context, servers []MCPServer, maxChar int) *Tools {
	t := &Tools{route: map[string]route{}, maxChar: maxChar}
	for _, s := range servers {
		url := os.Getenv(s.URLEnv)
		if url == "" {
			log.Printf("MCP server %q skipped: env %s is empty", s.Name, s.URLEnv)
			continue
		}
		hc := &http.Client{Transport: http.DefaultTransport}
		if s.BearerEnv != "" {
			if tok := os.Getenv(s.BearerEnv); tok != "" {
				hc.Transport = bearerTransport{token: tok, base: http.DefaultTransport}
			}
		}
		client := mcp.NewClient(&mcp.Implementation{Name: "matrix-ai-bot", Version: "0.1.0"}, nil)
		// URLs may contain secrets: never log them.
		session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: hc}, nil)
		if err != nil {
			log.Printf("MCP server %q connect failed: %v", s.Name, redact(err, url))
			continue
		}
		res, err := session.ListTools(ctx, nil)
		if err != nil {
			log.Printf("MCP server %q list tools failed: %v", s.Name, redact(err, url))
			continue
		}
		n := 0
		for _, tool := range res.Tools {
			if !toolAllowed(s, tool) {
				continue
			}
			name := s.Name + "_" + tool.Name
			t.defs = append(t.defs, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        name,
					"description": tool.Description,
					"parameters":  sanitizeSchema(tool.InputSchema),
				},
			})
			t.route[name] = route{session: session, tool: tool.Name}
			n++
		}
		log.Printf("MCP server %q: %d tools enabled", s.Name, n)
	}
	return t
}

func redact(err error, secret string) string {
	return strings.ReplaceAll(err.Error(), secret, "<redacted>")
}

func toolAllowed(s MCPServer, tool *mcp.Tool) bool {
	if len(s.AllowedTools) > 0 {
		found := false
		for _, a := range s.AllowedTools {
			if a == tool.Name {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if s.ReadOnly && (tool.Annotations == nil || !tool.Annotations.ReadOnlyHint) {
		return false
	}
	return true
}

// sanitizeSchema converts an MCP input schema to something strict OpenAI
// compatible endpoints (notably Gemini) accept.
func sanitizeSchema(in any) map[string]any {
	m := toMap(in)
	if m == nil {
		m = map[string]any{}
	}
	out, _ := cleanSchema(m).(map[string]any)
	if out["type"] == nil {
		out["type"] = "object"
	}
	if out["type"] == "object" && out["properties"] == nil {
		out["properties"] = map[string]any{}
	}
	return out
}

func toMap(in any) map[string]any {
	switch v := in.(type) {
	case map[string]any:
		return v
	case nil:
		return nil
	default:
		b, err := jsonMarshal(v)
		if err != nil {
			return nil
		}
		var m map[string]any
		if jsonUnmarshal(b, &m) != nil {
			return nil
		}
		return m
	}
}

var droppedKeys = map[string]bool{"$schema": true, "additionalProperties": true, "$id": true, "$defs": true, "definitions": true}

func cleanSchema(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			if droppedKeys[k] {
				continue
			}
			out[k] = cleanSchema(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = cleanSchema(val)
		}
		return out
	default:
		return v
	}
}

func (t *Tools) Defs() []map[string]any { return t.defs }

// Call executes a tool call and returns text for the model. Errors are
// returned as text so that the model can react to them.
func (t *Tools) Call(ctx context.Context, name string, args map[string]any) string {
	r, ok := t.route[name]
	if !ok {
		return "error: unknown tool " + name
	}
	res, err := r.session.CallTool(ctx, &mcp.CallToolParams{Name: r.tool, Arguments: args})
	if err != nil {
		return "error: " + err.Error()
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
			sb.WriteString("\n")
		} else {
			sb.WriteString(fmt.Sprintf("[%T content omitted]\n", c))
		}
	}
	out := strings.TrimSpace(sb.String())
	if res.IsError {
		out = "error: " + out
	}
	if t.maxChar > 0 && len(out) > t.maxChar {
		out = out[:t.maxChar] + "\n...[truncated]"
	}
	return out
}

// Close terminates all MCP sessions.
func (t *Tools) Close() {
	seen := map[*mcp.ClientSession]bool{}
	for _, r := range t.route {
		if !seen[r.session] {
			seen[r.session] = true
			r.session.Close()
		}
	}
}
