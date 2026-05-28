# AGENTS.md — Agent Harness

> **IMPORTANT:** `UAT/` is the user's test area. Do NOT read, modify, or reference any files under `UAT/` unless explicitly instructed.

---

## What it does

The Substrate is a minimal autonomous agent runtime written in Go — single binary, stdlib only, zero external dependencies. Messages arrive via HTTP webhook, are queued FIFO, and processed by an agent that iterates in a tool-call loop against an OpenAI-compatible LLM. Each channel has its own isolated, persisting JSON session with automatic context summarization when conversations grow too long.

## Documentation index

| File | Purpose |
|---|---|
| [`docs/getting-started.md`](docs/getting-started.md) | Compile, run, and use the CLI client |
| [`docs/architecture.md`](docs/architecture.md) | Source-tree module map — find any file or function |
| [`docs/TODO.md`](docs/TODO.md) | Open issues, quality gaps, and missing tests |
| [`docs/DONE.md`](docs/DONE.md) | Completed items (historical record) |
| [`docs/FUTURE.md`](docs/FUTURE.md) | Roadmap for features blocked on TODO completion |
| [`docs/prompts/`](docs/prompts/) | User-facing build prompts (AGENTS_LAYER, STANDARDS, TESTS, VERIFICATION, PHASE) |
| [`config.ini-example`](config.ini-example) | Full configuration reference |

Use `docs/architecture.md` to navigate the source code.
