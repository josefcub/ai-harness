# Agent Test Refactoring — Progress Report

## Status: COMPLETE — All 14 behavioral tests refactored, 1 redundant test removed

Tests identified in `docs/refactor_agent_tests.md` for refactoring from surface assertions to deep data-structure verification.

## What "deep" means (from refactor_agent_tests.md)

- **Exact message count** at each stage (not just "some number")
- **Exact role at each index** (e.g., `sess.Messages[0].Role == "user"`)
- **Exact content** on every message (not substring checks)
- **Exact tool call IDs** preserved between assistant→tool messages
- **Exact tool call fields** (ID, Name, Arguments) on assistant messages
- **Exact output strings** (not `strings.Contains` / `strings.HasPrefix` / `strings.HasSuffix`)
- **Error structure** (iteration context, error message content)

## Tests refactored ✅ (15 of 15)

### Batch 1
| Test | Before | After |
|---|---|---|
| `TestProcessPlainTextResponse` | `strings.Contains(output, "...")` | Exact output match, exact role/content on each message, ToolCalls nil check |
| `TestProcessToolCallLoop` | `strings.Contains` on output | Exact roles at each index, exact tool call ID/Name/Args, exact ToolCallID on tool message, exact output string |

### Batch 2
| Test | Before | After |
|---|---|---|
| `TestProcessMaxIterations` | Removed (redundant with TestProcessMaxIterationsSyntheticClosing which covers the same assertions) |
| `TestProcessMaxIterationsSyntheticClosing` | `strings.Contains` on output + content, `LastMessage()` | Exact message count (8), exact roles at each index, sequential call_0→call_2 IDs, exact synthetic closing content, exact output string |

### Batch 3
| Test | Before | After |
|---|---|---|
| `TestProcessMaxIterationsNormalExitUnaffected` | `strings.Contains(output, "tool call limit")` negative check | Removed redundant check; exact output match already covers |
| `TestProcessToolError` | `strings.Contains` on tool error content + output | Exact tool error content `"unknown tool: nonexistent_tool"`, exact output string |
| `TestProcessLLMError` | `strings.Contains(err.Error(), "connection refused")` | Exact error string `"LLM call failed (iteration 0): connection refused"` |
| `TestProcessPartialResponse` | `strings.Contains` on error + output | Exact error string with iteration context, exact output with reasoning prefix |

### Batch 4
| Test | Before | After |
|---|---|---|
| `TestSummarizationTriggersAtThreshold` | Loop with `strings.Contains` on ReasoningContent + loop checking old messages gone | Exact message count (4), index-based role/content verification on each message |
| `TestSummarizationFailure` | `strings.Contains` on error + loop checking tool message | Exact error string, exact message count (4), index-based role/content on each message |
| `TestProcessReasoningOutputFormat` | `strings.HasPrefix` + `strings.HasSuffix` on output | Exact output string + session state verification (reasoning content, assistant fields) |
| `TestMultipleToolCallsInOneTurn` | Three `strings.Contains(output, "...")` checks | One exact output match + full session structure (5 messages, tool call IDs, ToolCallID refs) |

### Batch 5
| Test | Before | After |
|---|---|---|
| `TestProcessWithImageAttachment` | `strings.Contains(output, "I see a photo of a cat.")` | Exact output match |
| `TestProcessMultipleDifferentToolCalls` | Five `strings.Contains(output, "...")` checks | One exact output match |
| `TestProcessImageAttachmentWithToolCall` | Attachment check + LLM multimodal verification + message count | Added exact role/content on each message index (0-3) |
| `TestProcessReasoningPreservedAfterToolLoop` | Two `strings.Contains(output, "[Reasoning: ...]")` checks | One exact output match with full reasoning/tool/result structure |

## Tests already deep (no changes needed)

The following tests were already using deep assertions and were left unchanged:
- `TestSplitMessages` / `TestSplitMessagesKeepZero` / `TestSplitMessagesKeepAll`
- `TestSystemPromptPreserved` — checks LLM message roles and JSON content
- `TestProcessReasoningContentRecordedInSession` — exact role/content/reasoning checks
- `TestProcessReasoningOnlyResponse` — exact output string match
- `TestConvertLLMToolCalls*` (3 tests) — exact field checks
- `TestConvertSessionToolCalls*` (3 tests) — exact field checks
- `TestAccumulateOutput*` (4 tests) — exact output string checks
- `TestConvertMessageWithToolCallID` — exact LLM message verification
- `TestConvertMessageWithBothToolCallsAndToolCallID` — exact LLM message verification
- `TestToMultimodalMessageEmptyContent` — exact content parts verification
- `TestToMultimodalMessageMultipleAttachments` — exact parts count/types
- `TestToMultimodalMessageToolCallsWithAttachments` — exact multimodal + tool call verification
- `TestParseToolResult*` (11 tests) — exact field checks
- `TestConvertMessage*` (4 tests) — exact field checks
- `TestToMultimodalMessage*` (3 tests) — exact parts checks
- `TestApplyToolCalls*` (3 tests) — exact field checks

## Verification

All 69 tests pass (all behavioral tests now use deep assertions):
```
cd /Users/josefcub/Source/ai/ai-runtime/src && go test ./agent/... -v -count=1
# PASS (69 tests, ~0.4s)

cd /Users/josefcub/Source/ai/ai-runtime/src && go test -race ./agent/...
# ok  github.com/agent-project/harness/agent  1.3s (no races)

cd /Users/josefcub/Source/ai/ai-runtime/src && go vet ./agent/...
# (clean)
```

## Summary

| Category | Count |
|---|---|
| Fully refactored ✅ | 14 |
| Removed (redundant) | 1 |
| Already deep (no changes needed) | ~47 |
| **Total behavioral tests** | **14** |
