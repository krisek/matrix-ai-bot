package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// MCPServer describes one MCP server reachable over streamable HTTP.
// URL and bearer token are read from environment variables so that they can
// live in Kubernetes Secrets (URLs may contain a secret path).
type MCPServer struct {
	Name         string   `json:"name"`
	URLEnv       string   `json:"urlEnv"`
	BearerEnv    string   `json:"bearerEnv,omitempty"`
	ReadOnly     bool     `json:"readOnly,omitempty"`
	AllowedTools []string `json:"allowedTools,omitempty"`
}

type Config struct {
	MatrixURL, MatrixUser, MatrixPass string
	Callword                          string
	LLMBaseURL, LLMModel, LLMAPIKey   string
	SystemPrompt                      string
	MaxHistory                        int
	MaxToolRounds                     int
	MaxToolResultChars                int
	LLMTimeout                        time.Duration
	MCPServers                        []MCPServer
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func LoadConfig() (*Config, error) {
	c := &Config{
		MatrixURL:          os.Getenv("MATRIX_URL"),
		MatrixUser:         os.Getenv("MATRIX_USER"),
		MatrixPass:         os.Getenv("MATRIX_PASS"),
		Callword:           getenv("CALLWORD", "@ai"),
		LLMBaseURL:         getenv("LLM_BASE_URL", "https://generativelanguage.googleapis.com/v1beta/openai"),
		LLMModel:           getenv("LLM_MODEL", "gemini-2.5-flash"),
		LLMAPIKey:          os.Getenv("LLM_API_KEY"),
		SystemPrompt:       getenv("SYSTEM_PROMPT", "You are a helpful assistant in a Matrix chat. Keep answers concise."),
		MaxHistory:         getenvInt("MAX_HISTORY", 20),
		MaxToolRounds:      getenvInt("MAX_TOOL_ROUNDS", 8),
		MaxToolResultChars: getenvInt("MAX_TOOL_RESULT_CHARS", 8000),
		LLMTimeout:         time.Duration(getenvInt("LLM_TIMEOUT_SECONDS", 120)) * time.Second,
	}
	if c.MatrixURL == "" || c.MatrixUser == "" || c.MatrixPass == "" || c.LLMAPIKey == "" {
		return nil, fmt.Errorf("MATRIX_URL, MATRIX_USER, MATRIX_PASS and LLM_API_KEY are required")
	}
	if path := os.Getenv("SYSTEM_PROMPT_FILE"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read SYSTEM_PROMPT_FILE: %w", err)
		}
		if p := strings.TrimSpace(string(b)); p != "" {
			c.SystemPrompt = p
		}
	}
	if path := os.Getenv("MCP_CONFIG"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read MCP_CONFIG: %w", err)
		}
		if err := json.Unmarshal(b, &c.MCPServers); err != nil {
			return nil, fmt.Errorf("parse MCP_CONFIG: %w", err)
		}
	}
	return c, nil
}
