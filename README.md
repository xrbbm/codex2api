# codex2api

A lightweight proxy that exposes OpenAI Codex as an **Anthropic Messages API** and **OpenAI Chat Completions API** compatible endpoint.

[中文文档](README_ZH.md)

---

## How It Works

codex2api sits between your AI client and the Codex backend. It accepts standard Anthropic or OpenAI requests, translates them to the Codex wire format, and streams the response back in the original client's expected format — including full SSE streaming support.

```
Claude Code / any Anthropic client
        │  POST /v1/messages
        ▼
  codex2api (localhost)
        │  POST https://chatgpt.com/backend-api/codex/responses
        ▼
    OpenAI Codex
```

---

## Features

- **Dual protocol support** — Anthropic Messages API (`/v1/messages`) and OpenAI Chat Completions API (`/v1/chat/completions`)
- **Streaming** — Server-Sent Events (SSE) for both protocols
- **Token management** — upload `auth.json`, automatic refresh every 8 hours
- **API key auth** — generate scoped keys via the admin panel, no direct token exposure
- **Web admin panel** — browser UI at `/admin` for all configuration
- **Proxy support** — respects `HTTP_PROXY` / `HTTPS_PROXY` environment variables
- **Zero dependencies at runtime** — single static binary, SQLite embedded (no CGO)

---

## Quick Start

### 1. Download

Grab a pre-built binary from the [releases page](../../releases) or build from source (see [Building](#building)).

```bash
chmod +x codex2api-linux-amd64
./codex2api-linux-amd64
```

Default port is **13698**. Override with `-p <port>` or `PORT=<port>`.

### 2. Open the Admin Panel

Navigate to `http://localhost:13698/admin` in your browser.

1. **Set a password** (first run only, minimum 8 characters)
2. **Upload `auth.json`** — paste the contents of your `~/.codex/auth.json`
3. **Generate an API key** — click "Generate Key", copy the `sk-codex-...` value

### 3. Connect Your Client

**Claude Code / Anthropic clients:**

```bash
export ANTHROPIC_BASE_URL=http://localhost:13698
export ANTHROPIC_API_KEY=sk-codex-<your-key>
claude
```

**OpenAI clients:**

```bash
export OPENAI_BASE_URL=http://localhost:13698/v1
export OPENAI_API_KEY=sk-codex-<your-key>
```

---

## API Reference

### POST /v1/messages (Anthropic)

```bash
curl http://localhost:13698/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: sk-codex-<your-key>" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "gpt-5.2",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

Add `"stream": true` for SSE streaming.

### POST /v1/chat/completions (OpenAI)

```bash
curl http://localhost:13698/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-codex-<your-key>" \
  -d '{
    "model": "gpt-5.2",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "Hello!"}
    ]
  }'
```

Add `"stream": true` for SSE streaming.

### GET /v1/models

Returns the list of available model IDs configured in the admin panel.

### GET /health

```json
{"status": "ok", "tokens_configured": true, "local_auth": false}
```

---

## Configuration

All settings can be changed in the admin panel or via environment variables.

| Admin panel field | Environment variable | Default |
|---|---|---|
| Codex base URL | `CODEX_BASE` | `https://chatgpt.com/backend-api/codex` |
| Refresh URL | `CODEX_REFRESH_URL` | `https://auth.openai.com/oauth/token` |
| Client ID | `CODEX_CLIENT_ID` | `app_EMoamEEZ73f0CkXa**hr***` |
| Default model | `CODEX_DEFAULT_MODEL` | `gpt-5.2` |
| Data directory | `DATA_DIR` | `~/.codex2api` |
| Port | `PORT` | `13698` |

**Models list** — pipe-separated list of model IDs shown to clients (e.g. `gpt-5.2|gpt-4.5`). The first entry is also used as the active Codex model.

**Local auth** — when enabled, codex2api reads tokens directly from `~/.codex/auth.json` without requiring a manual upload.

**Proxy** — set `HTTPS_PROXY` or `HTTP_PROXY` to route Codex API calls through a proxy.

---

## Building

Requires Go 1.21+. For smaller binaries, install [UPX](https://upx.github.io/) before building.

```bash
# Build both platforms (outputs to dist/)
make all

# Build for current platform
make build

# Individual targets
make linux-amd64
make linux-arm64
```

See [CLAUDE.md](CLAUDE.md) for details on the binary size optimization strategy (15 MB → ~3.5 MB).

---

## Data Storage

All persistent data is stored in a single SQLite database at `~/.codex2api/data.db` (or the path set by `DATA_DIR`):

- Admin password hash
- Codex access/refresh tokens
- Generated API keys
- Configuration values

---

## License

MIT
