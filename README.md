# matrix-ai-bot

Matrix bot that answers messages starting with a call word (default `@ai`) using an
OpenAI-compatible LLM (default: Gemini) and MCP tools over **streamable HTTP**.
Single static Go binary, no `aichat` or other CLI needed.

- `@ai <question>` asks; every room has its own memory (last `maxHistory` messages).
- `@ai /reset` clears the room's memory.
- `@ai /listen` makes the bot answer **every** message in that room (no callword needed);
  `@ai /silence` turns it off again. Commands always need the callword. The mode is per room
  and, like the memory, is lost on restart.
- Works in every room the bot is in; it auto-joins on invite.
- Tool calls loop (max `maxToolRounds`) until the model produces a final answer.
- MCP tools are writable by default; set `readOnly: true` on a server to expose only
  tools annotated `readOnlyHint`, or use `allowedTools`.

## Configuration (env)

| Variable | Default | |
|---|---|---|
| `MATRIX_URL`, `MATRIX_USER`, `MATRIX_PASS` | required | same Secret as wisebalance-bot |
| `LLM_API_KEY` | required | |
| `LLM_BASE_URL` | Gemini OpenAI endpoint | |
| `LLM_MODEL` | `gemini-2.5-flash` | |
| `CALLWORD` | `@ai` | |
| `SYSTEM_PROMPT` / `SYSTEM_PROMPT_FILE` | short default | |
| `MAX_HISTORY`, `MAX_TOOL_ROUNDS`, `MAX_TOOL_RESULT_CHARS`, `LLM_TIMEOUT_SECONDS` | 20, 8, 8000, 120 | |
| `MCP_CONFIG` | none | JSON file: `[{"name","urlEnv","bearerEnv","readOnly","allowedTools"}]` |

MCP URLs and tokens are read from the env vars named in `urlEnv` / `bearerEnv`
(they come from Secrets; URLs are never logged).

## Helm

Create the secret in the namespace of wisebalance-bot:

```sh
kubectl create secret generic matrix-ai-bot-secrets \
  --from-literal=LLM_API_KEY=... \
  --from-literal=MCP_URL_AUDIOLOADER=https://al.example.com/mcp/SECRET
helm upgrade --install matrix-ai-bot helm -f my-values.yaml
```

Example my-values.yaml:

```yaml
mcp:
  servers:
    - name: audioloader
      urlSecret: {name: matrix-ai-bot-secrets, key: MCP_URL_AUDIOLOADER}
```

Tool names are exposed to the model as `<server>_<tool>`.

## Build

GitHub Actions builds `linux/amd64,linux/arm64` to `ghcr.io/krisek/matrix-ai-bot` on push to `main`.
Locally: `go test ./...`.
