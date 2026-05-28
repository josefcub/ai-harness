# DONE.md 

## This file tracks what items have been completed from TODO.md by removing them from TODO.md

# src/agent.go:

- [x] `agent.go:62-233` `Process()` — 172 lines, handles user message creation, channel logging, tool-call loop, LLM calls, tool execution, output accumulation, session state management, summarization triggering, and synthetic closing message generation.
  - It contains the following behaviors, each one of which needs to be tested:
    - [x] ReasoningContent recorded in session on normal (non-error) response
    - [x] ReasoningContent-only response (Content empty, ReasoningContent present) produces correct output format
    - [x] Output format for reasoning in normal flow: `[Reasoning: ...]\n` prefix is correct
    - [x] Multiple tool calls with different tool names (currently only `echo` twice is tested)
    - [x] Image attachment + tool call in same message
    - [x] ReasoningContent preserved in final assistant session message after tool loop
  - It violates SRP and is too long:
    - [x] Duplicated content/reasoning accumulation (lines 107-123 vs 126-140): partial-response and normal paths write identical `[Reasoning: ...]` logic — extract to helper
    - [x] Duplicated tool-call-to-session conversion (lines 157-166): same struct-copy pattern repeated in `convertMessage` and `toMultimodalMessage` — extract `convertToolCalls()` helper
    - [x] Function should decompose into: message creation, tool-call loop iteration, LLM dispatch, output accumulation

- [x] `agent.go:255-360` `summarizeContext()` — summarization flow.
  - It contains the following behaviors, each one of which needs to be tested:
    - [x] Empty Content fallback to ReasoningContent (lines 314-316)
    - [x] Summary message structure: Role=Assistant, Content="", ReasoningContent="[Summary...]\n<text>"
    - [x] Summarization with attachment-protected messages through full flow
  - Code quality issues:
    - [x] Duplicated error-handling pattern (lines 294-310 vs 317-332): both do `logger.Error` + `channelLogger.Log` + session append — extract `logAndRecordSummarizationError()`
    - [x] Repeated channelLogger.Log pattern with identical Entry struct appears 4 times — deduplicate

- [x] `agent.go:362-376` `totalTokens()` — token estimation.
  - It contains the following behaviors, each one of which needs to be tested:
    - [x] Token count for attachments (`* attachmentTokenCost`)
    - [x] Token count for tool calls (function name + arguments)
    - [x] Token count for ReasoningContent (now counted — preserve_thinking includes it in context)
    - [x] Token count for ToolCallID
    - [x] System prompt token estimation (`/ 3`)

- [x] `agent.go:381-406` `splitMessages()` — message splitting for summarization.
  - It contains the following behaviors, each one of which needs to be tested:
    - [x] Attachment-protected messages moved from old to recent (lines 390-403)
    - [x] Mixed scenario: some old messages with attachments, some without

- [x] `agent.go:411-437` `parseToolResult()` — tool result parsing with attachment extraction.
  - It contains the following behaviors, each one of which needs to be tested:
    - [x] Valid JSON with `__attachment` key returns text + attachment
    - [x] Valid JSON without `__attachment` returns raw result
    - [x] Non-JSON input returns raw result
    - [x] JSON with `__attachment` but no `text` field returns `""` + attachment
    - [x] Invalid `__attachment` JSON (marshal/unmarshal error) returns raw result

- [x] `agent.go:455-528` `convertMessage()` / `toMultimodalMessage()` — message conversion.
  - It contains the following behaviors, each one of which needs to be tested:
    - [x] convertMessage with ToolCallID set
    - [x] convertMessage with both ToolCalls and ToolCallID
    - [x] toMultimodalMessage with empty Content (image-only)
    - [x] toMultimodalMessage with multiple attachments
    - [x] toMultimodalMessage with tool calls + attachments
  - Code quality issues:
    - [x] Duplicated tool-call conversion: lines 465-474 and 511-520 are identical — extract shared `convertToolCallsToLLM()`

---

## agent/agent_test.go

- [x] `agent_test.go` — existing tests use surface assertions (`err == nil`, output string prefix checks) rather than deep data-structure verification. All tests should verify returned fields, message roles, tool call IDs, and session state.

---

**12-parameter constructor.** `agent.New()` takes twelve arguments:

```go
func New(client ChatClient, reg *tools.Registry,
    maxToolIterations, contextTokens int,
    summarizeThreshold float64, summarizeKeepRecent, maxTokens int,
    summaryPrompt string,
    logToolCalls, logAgentReasoning bool,
    channelLogger *channellog.Logger, logger *log.Logger) *Agent
```

That's not a constructor — it's a configuration object masquerading as a parameter list. Fowler would call this a "Large Class" and recommend a Builder pattern or a dedicated configuration struct.

---

**`summarizeContext` at 69 lines does five things:** logs, splits messages, calls the LLM, records errors, mutates state. It's a "God Fragment" — not yet a God Object, but close.

---

**`file_tools.go` at 683 lines with 13 functions** is a single concern (file operations) treated as one monolithic file rather than separate modules for `view`, `write`, `append`, `edit`, `ls`, `glob`, `grep`. Each of those could own its own package.

---

## worker/worker.go

- [x] `worker.go:109-172` `processMessage()` — per-message processing with session save and callback dispatch.
  - It contains the following behaviors, each one of which needs to be tested:
    - [x] Session state after error: user message present in session
    - [x] Session saved on error path (only callback is asserted currently)
    - [x] buildSystemPrompt with AGENTS.md file present
    - [x] buildSystemPrompt with all 5 prompt files simultaneously
    - [x] buildSystemPrompt file delimiter format: `--- END FILENAME ---`
  - Code quality issues:
    - [x] Duplicated session save + error log (lines 140-148 vs 152-159) — extract `saveSession(sess)`
    - [x] Duplicated callback send structure (lines 132-138 vs 162-170) — extract `sendCallback()`

- [x] `worker.go:85-106` `Run()` — worker poll loop.
  - It contains the following behaviors, each one of which needs to be tested:
    - [x] Message enqueued after worker starts mid-poll is eventually picked up

---
## worker/worker_test.go

- [ ] `worker_test.go:396-437` `TestWorker_ConcurrentSafety` — test quality issues:
  - [ ] Dead code: `processed` (atomic.Int32) declared and unused via `_ = processed`
  - [ ] Dead code: `origProcess` captured and unused via `_ = origProcess`
  - [ ] Test replaces `w.processor` directly, bypassing constructor — tests internals not behavior

---

## main.go

- [x] `main.go:25-182` `main()` — 157 lines. Acceptable for bootstrap, but has extractable concerns:
  - [x] Shutdown drain loop (lines 161-170): extract to `drainPending(q, sessions, logger)` for testability

---

## main_test.go

Refactored entirely instead of doing the thing.  Ugh.

---

## config/config.go

- [x] `config.go:112` `Load()` — Bash.Banned default is a 60+ token inline comma-separated list.
  - Code quality issues:
    - [x] Inline list should be extracted to a `var` or `const` for readability and independent **testability**

- [x] `config.go:118-175` `Validate()` — 58 lines with 12+ individual `if` checks.
  - Code quality issues:
    - [x] Violates SRP — should split into `validateLLM()`, `validateServer()`, `validateQueue()`, `validateLogging()`, `validateBash()` sub-methods for testability and maintainability

- [x] `config.go:178-237` helper functions.
  - It contains the following behaviors, each one of which needs to be tested:
    - [x] `strListDefault()`: empty default, single item, comma-separated with whitespace, mixed case lowercasing, empty entries filtered out, missing section/key falls back to split default

---

## config/config_test.go

- [x] `TestLoadFullConfig` — missing assertions:
  - [x] `cfg.Bash.Enabled`, `cfg.Bash.Timeout`, `cfg.Bash.MaxOutput`, `cfg.Bash.Banned`
  - [x] `cfg.Paths.ChannelLogDir`

- [x] `TestLoadDefaults` — missing assertions:
  - [x] `cfg.Paths.ChannelLogDir`, `cfg.Bash.Enabled`, `cfg.Bash.Timeout`, `cfg.Bash.MaxOutput`, `cfg.Bash.Banned`
  - [x] `cfg.LLM.SummarizeThreshold`, `cfg.LLM.SummarizeKeepRecent`

- [x] Missing validation tests:
  - [x] `llm.max_tokens <= 0`
  - [x] `llm.timeout <= 0`
  - [x] `llm.max_tool_iterations <= 0`
  - [x] `tools.bash.timeout <= 0`
  - [x] `tools.bash.max_output <= 0`
  - [x] `server.port = 0` (lower boundary)
  - [x] `summarize_threshold = 0` (lower boundary)

---




