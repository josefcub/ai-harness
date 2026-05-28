# Architecture — Module Map

This file maps every package under `src/` to its purpose and entry points. Use it to find the code you need.

## High-level flow

```
Webhook (webhook.Server)
  └─→ Queue (queue.Queue, FIFO, per-channel backpressure)
       └─→ Worker (worker.Worker, polls queue)
            └─→ Agent (agent.Agent, tool-call loop)
                 ├─→ LLM (llm.Client, OpenAI-compatible)
                 ├─→ Tools (tools.Registry + tool packages)
                 └─→ Session (session.Manager, flat JSON persistence)
```

## Packages

### `src/` — main (entry point)

**`main.go`** — Bootstrap: loads config, initializes all subsystems, starts webhook + worker, handles graceful shutdown.

**`drain.go`** — Queue drain logic (called during shutdown to persist pending messages without LLM calls).

### `src/agent/` — the agent

| File | Purpose |
|---|---|
| `agent.go` | Core agent: runs the tool-call loop against the LLM, manages message flow |
| `summarizer.go` | Context summarization: compresses old messages when approaching context limits |
| `summary.go` | Summary prompt template and formatting |
| `summary.md` | Raw summary prompt text (injected at compile time) |

Key types: `Agent`, `Process()`, `summarizeContext()`

### `src/config/` — configuration

| File | Purpose |
|---|---|
| `config.go` | Config struct and `Load()` / `Validate()` |
| `ini_parser.go` | Custom INI parser with multiline (`"""`) string support |

### `src/llm/` — LLM client

| File | Purpose |
|---|---|
| `client.go` | OpenAI-compatible HTTP client with SSE streaming support |

Key types: `ChatClient`, `Chat()`, `ChatStream()`

### `src/tools/` — tool registry and implementations

| File | Purpose |
|---|---|
| `tools.go` | Tool registry: `Register()`, `Definitions()`, `Dispatch()` |
| `file_tools.go` | `read_file`, `write_file`, `append_file`, `edit_file`, `list_files` |
| `glob_tools.go` | `glob` — pattern-based file search |
| `grep_tools.go` | `grep` — regex and plain text content search |
| `bash_tool.go` | `bash` — sandboxed shell command execution |
| `web_tools.go` | `fetch`, `download` — HTTP operations |
| `image_tool.go` | `view_image` — load and base64-encode images for vision models |

### `src/sandbox/` — path sandboxing

| File | Purpose |
|---|---|
| `sandbox.go` | `ResolvePath()` — blocks absolute paths, `..` traversal, and symlink escapes |

### `src/session/` — session persistence

| File | Purpose |
|---|---|
| `session.go` | `Manager` with `Get()`, `Save()`, `LoadAll()`, `SaveAll()`, `DrainAndSave()` — flat JSON with atomic writes |

### `src/webhook/` — HTTP webhook ingress

| File | Purpose |
|---|---|
| `server.go` | HTTP server: validates inbound requests, enqueues messages, handles per-channel backpressure |
| `callback.go` | `sendCallback()` — posts aggregated output to the caller |

### `src/worker/` — message worker

| File | Purpose |
|---|---|
| `worker.go` | `Worker` with `Run()` (poll loop) and `processMessage()` (session save + callback dispatch) |

### `src/queue/` — FIFO queue

| File | Purpose |
|---|---|
| `queue.go` | Thread-safe FIFO with configurable max depth and per-channel backpressure |

### `src/channellog/` — conversation logging

| File | Purpose |
|---|---|
| `channellog.go` | Writes per-channel JSONL logs to `paths.channel_log_dir` |

### `src/log/` — logging

| File | Purpose |
|---|---|
| `logger.go` | File-based logger with configurable log levels (debug, info, warn, error) |

### `src/imageutil/` — image utilities

| File | Purpose |
|---|---|
| `imageutil.go` | `DetectMIME()` and `MaxImageSize` constant |

### `src/testutil/mockllm/` — testing fixtures

| File | Purpose |
|---|---|
| `client.go` | `MockClient` — queue responses/errors for unit tests against `agent` or `llm` code |
| `server.go` | `Server` — in-memory mock LLM HTTP server for integration tests |

### `src/cmd/client/` — CLI test client

| File | Purpose |
|---|---|
| `client.go` | Sends messages to the harness webhook, optionally waits for callback response |

### `src/cmd/tools/coverage/` — coverage viewer

| File | Purpose |
|---|---|
| `main.go` | Renders `go test -coverprofile` output as annotated source code |
