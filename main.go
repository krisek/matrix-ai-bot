package main

import (
	"context"
	"log"
	"time"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	tools := ConnectTools(ctx, cfg.MCPServers, cfg.MaxToolResultChars)
	cancel()

	chat := NewChat(cfg, NewLLM(cfg, tools))
	log.Fatal(RunBot(cfg, chat))
}
