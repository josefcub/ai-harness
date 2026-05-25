## Task: Refactor agent_test.go tests from surface to deep assertions

### Context
The file `/Users/josefcub/Source/ai/ai-runtime/src/agent/agent_test.go` contains tests for the agent package. The TODO.md says: "existing tests use surface assertions (`err == nil`, output string prefix checks) rather than deep data-structure verification. All tests should verify returned fields, message roles, tool call IDs, and session state."

### What "surface assertions" means (to AVOID)
- `if !strings.Contains(output, "expected text")` — checking output string contains substring
- `if err == nil` — only checking no error, not what happened
- `if output != ""` — surface empty check
- `if !strings.HasPrefix(output, "[Reasoning:` — string prefix check on output

### What "deep data-structure verification" means (TO DO)
- Verify exact session message count and roles at each index
- Verify exact Content, ReasoningContent, ToolCalls, ToolCallID on each message
- Verify tool call IDs are preserved and match between assistant and tool messages
- Verify LLM messages sent (via mock client's LastMessages)
- Verify exact field values, not just presence of substrings
- For output: verify specific structural elements, not just substring presence

### Tests to refactor (in order, with specific changes):

#### 1. TestProcessPlainTextResponse (line ~111)
Current: checks `strings.Contains(output, "...")` and message count
Refactor:
- Keep message count check
- Add: verify sess.Messages[0].Role == "user", sess.Messages[0].Content == "What is 2+2?"
- Add: verify sess.Messages[1].Role == "assistant", sess.Messages[1].Content == "This is the final answer."
- Add: verify sess.Messages[1].ToolCalls is nil
- Replace output check with: verify output == "This is the final answer." (exact match, no substring)

#### 2. TestProcessToolCallLoop (line ~140)
Current: checks `strings.Contains(output, "[Tool Call: echo]")` etc.
Refactor:
- Keep message count check (4)
- Add: verify exact roles at each index: [0]=user, [1]=assistant(tool_calls), [2]=tool, [3]=assistant(final)
- Add: verify sess.Messages[0].Content == "Echo hello world"
- Add: verify sess.Messages[1].ToolCalls has 1 entry with ID="call_1", Name="echo", Args=`{"text":"hello world"}`
- Add: verify sess.Messages[2].Role=="tool", Content=="hello world", ToolCallID=="call_1"
- Add: verify sess.Messages[3].Content=="The echoed result is: hello world"
- Keep output checks but add: verify output contains "[Result: hello world]" (exact string, not substring)

#### 3. TestProcessMaxIterations (line ~187)
Current: counts tool-call assistant messages (good deep check) + output substring checks
Refactor:
- Keep tool-call message count check (already deep)
- Add: verify exact message count = 1 user + 3 assistant(tool) + 3 tool results = 7
- Add: verify user message content
- Add: verify each assistant tool-call message has correct tool ID and name
- Add: verify each tool message has correct ToolCallID
- Replace output substring checks with exact structural checks

#### 4. TestProcessMaxIterationsSyntheticClosing (line ~230)
Current: checks output contains "tool call limit", last message role/content
Refactor:
- Keep message count check (8) — already deep
- Add: verify exact roles at each index
- Add: verify synthetic closing message has exact content
- Add: verify all tool call IDs are sequential call_0 through call_2
- Replace output check with: verify output contains exact synthetic closing string

#### 5. TestProcessMaxIterationsNormalExitUnaffected (line ~285)
Current: checks output doesn't contain "tool call limit", last message content
Refactor:
- Keep last message check (already deep-ish)
- Add: verify exact message count = 4 (user + assistant(tool) + tool + assistant(final))
- Add: verify each message's exact content and roles
- Add: verify tool result content matches tool argument
- Replace output substring checks with exact assertions

#### 6. TestProcessToolError (line ~325)
Current: checks output contains "unknown tool" and "I apologize"
Refactor:
- Add: verify exact message sequence: user, assistant(tool=nonexistent_tool), tool(error), assistant(recovery)
- Add: verify tool error message content contains the error
- Add: verify tool call ID on the error tool message
- Replace output substring checks with exact content verification

#### 7. TestProcessLLMError (line ~366)
Current: checks error message contains "connection refused", message count = 1
Refactor:
- Keep message count check (already deep)
- Add: verify sess.Messages[0].Role == "user", Content == "Hello"
- Add: verify error contains "iteration 0" or similar context
- Replace substring error check with exact error structure verification

#### 8. TestProcessPartialResponse (line ~391)
Current: checks error/output contain substrings, verifies 2 messages
Refactor:
- Keep message count and role checks (already deep-ish)
- Add: verify exact Content and ReasoningContent on partial assistant message
- Add: verify error contains "partial response" and "iteration 0"
- Replace output substring checks with exact content verification

#### 9. TestSummarizationTriggersAtThreshold (line ~437)
Current: checks for summary marker in ReasoningContent, checks old messages removed
Refactor:
- Keep summary check (already deep-ish)
- Add: verify exact message count after summarization
- Add: verify summary message has correct ReasoningContent prefix
- Add: verify recent messages are preserved with exact content
- Replace substring checks with exact content verification

#### 10. TestSummarizationFailure (line ~500)
Current: checks error contains "context summarization failed", checks for tool message
Refactor:
- Keep error check (already deep-ish)
- Add: verify exact message sequence
- Add: verify tool message has exact error content
- Replace substring check with exact content verification

#### 11. TestProcessReasoningOnlyResponse (line ~831)
Current: checks output == expected (good!), checks reasoning content
Refactor:
- Already has good deep assertions
- Minor: verify output format exactly (already done)
- No major changes needed

#### 12. TestProcessReasoningOutputFormat (line ~866)
Current: checks output has prefix and suffix
Refactor:
- Replace Prefix/Suffix checks with exact string match
- Add: verify session state has correct reasoning content

#### 13. TestProcessMultipleDifferentToolCalls (line ~891)
Current: already has excellent deep assertions
Refactor:
- Already good — no changes needed

#### 14. TestProcessImageAttachmentWithToolCall (line ~996)
Current: already has good deep assertions
Refactor:
- Already good — minor: verify exact message count and roles

#### 15. TestProcessReasoningPreservedAfterToolLoop (line ~1052)
Current: checks reasoning content in session, checks output contains reasoning
Refactor:
- Keep session reasoning checks (already deep)
- Replace output substring checks with exact structural verification

### Guiding Principles (from Software Engineering at Google and The Pragmatic Programmer)

When refactoring these tests, apply these principles:

1. **Testable code is better code** (Google): Each test should verify a single, well-defined behavior. If a test needs to check multiple unrelated things, it should be split.

2. **Don't test implementation details** (Pragmatic Programmer): Assert on the *contract* (what the system does), not *how* it does it. Verify returned data structures, not string formatting of output buffers.

3. **The best tests are the ones you actually run** (Pragmatic Programmer): Keep tests fast and reliable. Use the mock client efficiently — don't queue unnecessary responses.

4. **Make it obvious** (Google): Test names should describe the scenario being tested. Assertions should fail with clear, actionable messages.

5. **DRY — Don't Repeat Yourself** (Google): Extract common verification patterns into helpers. If three tests check the same session structure, factor out the check.

6. **Orthogonality** (Google): Each test should exercise one independent behavior. Don't combine orthogonal concerns in a single test.

7. **Write tests that fail** (Pragmatic Programmer): Every assertion should be capable of catching a real regression. If an assertion can never fail, remove it.

8. **Change prevention** (Pragmatic Programmer): Good tests prevent regressions. Deep assertions catch subtle bugs that surface checks miss (e.g., wrong tool call ID, wrong message role).

9. **Single source of truth** (Google): The session data structure is the source of truth. Tests should verify the structure directly, not through string representations.

10. **Refactor continuously** (Pragmatic Programmer): While refactoring these tests, look for opportunities to improve test structure (table-driven tests, shared helpers, better naming) without changing behavior.

---

### Implementation approach
1. First, run `go test ./src/agent/...` to verify current tests pass
2. Refactor each test one at a time, running tests after each change
3. Use table-driven tests where multiple scenarios share the same verification logic
4. Extract a shared verification helper for common patterns (e.g., `verifySessionStructure`)
5. After all refactors, run full test suite + race detector

### Constraints
- Do NOT change the test names or remove existing test functions
- Do NOT change the test behavior (same scenarios, same mock setup)
- Only CHANGE the assertions to be deeper/more precise
- Keep the same mock client setup patterns
- Use `t.Fatalf` for fatal checks, `t.Errorf` for non-fatal
- Follow Go conventions: table-driven tests where appropriate
- Run `go vet ./src/agent/...` and `go test -race ./src/agent/...` after changes

### Files to modify
- `/Users/josefcub/Source/ai/ai-runtime/src/agent/agent_test.go`

### Files to read for context
- `/Users/josefcub/Source/ai/ai-runtime/src/agent/agent.go` (the code under test)
- `/Users/josefcub/Source/ai/ai-runtime/src/session/session.go` (for session types)
- `/Users/josefcub/Source/ai/ai-runtime/src/llm/llm.go` (for llm.Message types)

### Verification
After all changes:
1. `cd /Users/josefcub/Source/ai/ai-runtime/src && go test ./agent/... -v` — all pass
2. `cd /Users/josefcub/Source/ai/ai-runtime/src && go test -race ./agent/...` — no races
3. `cd /Users/josefcub/Source/ai/ai-runtime/src && go vet ./agent/...` — clean
