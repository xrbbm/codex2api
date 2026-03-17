# codex2api

将 OpenAI Codex 包装为 **Anthropic Messages API** 和 **OpenAI Chat Completions API** 兼容接口的轻量代理。

[English](README.md)

---

## 工作原理

codex2api 运行在本地，接收标准的 Anthropic 或 OpenAI 格式请求，将其转换为 Codex 协议后转发，再把响应翻译回客户端期望的格式——完整支持 SSE 流式输出。

```
Claude Code / 任意 Anthropic 或 OpenAI 客户端
        │  POST /v1/messages  或  /v1/chat/completions
        ▼
  codex2api（本地运行）
        │  POST https://chatgpt.com/backend-api/codex/responses
        ▼
    OpenAI Codex
```

---

## 功能特性

- **双协议支持** — Anthropic Messages API（`/v1/messages`）和 OpenAI Chat Completions API（`/v1/chat/completions`）
- **流式输出** — 两种协议均支持 Server-Sent Events（SSE）
- **Token 管理** — 上传 `auth.json`，每 8 小时自动刷新 Token
- **API Key 鉴权** — 在管理面板生成独立 Key，不直接暴露 Codex Token
- **Web 管理面板** — 浏览器访问 `/admin`，完成所有配置
- **代理支持** — 读取 `HTTP_PROXY` / `HTTPS_PROXY` 环境变量
- **零运行时依赖** — 单一静态二进制，SQLite 内嵌（无 CGO）

---

## 快速上手

### 1. 下载二进制

从 [Releases 页面](../../releases) 下载对应平台的二进制文件，或自行编译（见[编译](#编译)）。

```bash
chmod +x codex2api-linux-amd64
./codex2api-linux-amd64
```

默认端口为 **13698**，可用 `-p <端口>` 或 `PORT=<端口>` 覆盖。

### 2. 打开管理面板

在浏览器中访问 `http://localhost:13698/admin`。

1. **设置管理员密码**（首次运行，至少 8 位）
2. **上传 `auth.json`** — 粘贴 `~/.codex/auth.json` 的内容
3. **生成 API Key** — 点击"生成 Key"，复制 `sk-codex-...` 格式的 Key

### 3. 接入客户端

**Claude Code / Anthropic 客户端：**

```bash
export ANTHROPIC_BASE_URL=http://localhost:13698
export ANTHROPIC_API_KEY=sk-codex-<你的Key>
claude
```

**OpenAI 客户端：**

```bash
export OPENAI_BASE_URL=http://localhost:13698/v1
export OPENAI_API_KEY=sk-codex-<你的Key>
```

---

## API 接口

### POST /v1/messages（Anthropic 格式）

```bash
# 非流式
curl http://localhost:13698/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: sk-codex-<你的Key>" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "gpt-5.2",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "你好！"}]
  }'

# 流式（加 "stream": true）
curl http://localhost:13698/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: sk-codex-<你的Key>" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "gpt-5.2",
    "max_tokens": 1024,
    "stream": true,
    "messages": [{"role": "user", "content": "用 Go 写一个快速排序"}]
  }'
```

### POST /v1/chat/completions（OpenAI 格式）

```bash
# 非流式
curl http://localhost:13698/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-codex-<你的Key>" \
  -d '{
    "model": "gpt-5.2",
    "messages": [
      {"role": "system", "content": "你是一个有帮助的助手"},
      {"role": "user", "content": "1+1 等于几？"}
    ]
  }'

# 流式（加 "stream": true）
curl http://localhost:13698/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-codex-<你的Key>" \
  -d '{
    "model": "gpt-5.2",
    "stream": true,
    "messages": [{"role": "user", "content": "写一首关于秋天的诗"}]
  }'
```

### GET /v1/models

返回管理面板中配置的可用模型列表。

### GET /health

```json
{"status": "ok", "tokens_configured": true, "local_auth": false}
```

---

## 配置说明

所有配置均可在管理面板中修改，也可通过环境变量设置。

| 管理面板字段 | 环境变量 | 默认值 |
|---|---|---|
| Codex 接口地址 | `CODEX_BASE` | `https://chatgpt.com/backend-api/codex` |
| Token 刷新地址 | `CODEX_REFRESH_URL` | `https://auth.openai.com/oauth/token` |
| Client ID | `CODEX_CLIENT_ID` | `app_EMoamEEZ73f0CkXa**hr***` |
| 默认模型 | `CODEX_DEFAULT_MODEL` | `gpt-5.2` |
| 数据目录 | `DATA_DIR` | `~/.codex2api` |
| 监听端口 | `PORT` | `13698` |

**模型列表** — 用竖线分隔的模型 ID，展示给客户端（如 `gpt-5.2|gpt-4.5`）。第一个 ID 同时作为实际调用 Codex 时使用的模型。

**本地鉴权** — 启用后直接读取 `~/.codex/auth.json`，无需手动上传。适合本机使用。

**代理** — 设置 `HTTPS_PROXY` 或 `HTTP_PROXY` 可将 Codex 请求通过代理转发。

---

## 编译

需要 Go 1.21+。安装 [UPX](https://upx.github.io/) 可进一步压缩产物体积。

```bash
# 同时编译两个平台（输出到 dist/）
make all

# 仅编译当前平台
make build

# 单独编译
make linux-amd64
make linux-arm64
```

编译优化策略详见 [CLAUDE.md](CLAUDE.md)（产物体积从 15 MB 压缩至约 3.5 MB）。

---

## 数据存储

所有持久化数据存储在单一 SQLite 数据库 `~/.codex2api/data.db`（或 `DATA_DIR` 指定的路径）：

- 管理员密码哈希
- Codex access/refresh token
- 已生成的 API Key
- 配置项

---

## 许可证

MIT
