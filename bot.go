package main

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/matrix-org/gomatrix"
)

// Chat keeps per-room memory and answers prompts using the LLM.
type Chat struct {
	cfg   *Config
	llm   *LLM
	mu    sync.Mutex
	rooms map[string]*roomState
}

type roomState struct {
	sync.Mutex
	history []Message
}

func NewChat(cfg *Config, llm *LLM) *Chat {
	return &Chat{cfg: cfg, llm: llm, rooms: map[string]*roomState{}}
}

func (c *Chat) room(id string) *roomState {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.rooms[id]
	if !ok {
		r = &roomState{}
		c.rooms[id] = r
	}
	return r
}

// parseCommand returns the text after the callword. ok is false when the
// message is not addressed to the bot. The callword must be followed by
// whitespace, punctuation (":" or ",") or the end of the message.
func parseCommand(body, callword string) (rest string, ok bool) {
	b := strings.TrimSpace(body)
	if len(b) < len(callword) || !strings.EqualFold(b[:len(callword)], callword) {
		return "", false
	}
	rest = b[len(callword):]
	if rest != "" && !strings.ContainsAny(rest[:1], " \t\n:,") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(rest), ":,")), true
}

// Handle processes one message and returns the reply ("" means no reply).
func (c *Chat) Handle(ctx context.Context, roomID, body string) string {
	rest, ok := parseCommand(body, c.cfg.Callword)
	if !ok {
		return ""
	}
	r := c.room(roomID)
	r.Lock()
	defer r.Unlock()

	switch strings.ToLower(rest) {
	case "":
		return "Usage: " + c.cfg.Callword + " <question>  |  " + c.cfg.Callword + " /reset"
	case "/reset":
		r.history = nil
		return "Conversation reset."
	}

	history := append(append([]Message{}, r.history...), Message{Role: "user", Content: rest})
	answer, err := c.llm.Run(ctx, history)
	if err != nil {
		log.Printf("llm error in %s: %v", roomID, err)
		return "Sorry, that failed: " + err.Error()
	}
	if answer == "" {
		answer = "(empty response)"
	}
	r.history = append(history, Message{Role: "assistant", Content: answer})
	if max := c.cfg.MaxHistory; max > 0 && len(r.history) > max {
		r.history = r.history[len(r.history)-max:]
		// never start with an assistant message
		if r.history[0].Role == "assistant" {
			r.history = r.history[1:]
		}
	}
	return answer
}

func RunBot(cfg *Config, chat *Chat) error {
	cli, err := gomatrix.NewClient(cfg.MatrixURL, "", "")
	if err != nil {
		return err
	}
	resp, err := cli.Login(&gomatrix.ReqLogin{Type: "m.login.password", User: cfg.MatrixUser, Password: cfg.MatrixPass})
	if err != nil {
		return err
	}
	cli.SetCredentials(resp.UserID, resp.AccessToken)

	start := time.Now().UnixMilli()
	syncer := cli.Syncer.(*gomatrix.DefaultSyncer)

	syncer.OnEventType("m.room.member", func(ev *gomatrix.Event) {
		if ev.StateKey != nil && *ev.StateKey == cli.UserID && ev.Content["membership"] == "invite" {
			if _, err := cli.JoinRoom(ev.RoomID, "", nil); err != nil {
				log.Printf("join %s failed: %v", ev.RoomID, err)
			}
		}
	})

	syncer.OnEventType("m.room.message", func(ev *gomatrix.Event) {
		if ev.Sender == cli.UserID || ev.Timestamp < start {
			return // own message or history replayed at startup
		}
		body, _ := ev.Content["body"].(string)
		if body == "" {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), cfg.LLMTimeout*time.Duration(cfg.MaxToolRounds+1))
			defer cancel()
			reply := chat.Handle(ctx, ev.RoomID, body)
			if reply == "" {
				return
			}
			if _, err := cli.SendText(ev.RoomID, reply); err != nil {
				log.Printf("send to %s failed: %v", ev.RoomID, err)
			}
		}()
	})

	log.Printf("Bot running as %s, callword %q", resp.UserID, cfg.Callword)
	return cli.Sync()
}
