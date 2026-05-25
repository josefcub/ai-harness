package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agent-project/harness/llm"
	"github.com/agent-project/harness/session"
	"github.com/agent-project/harness/tools"
)

// mockCallResult holds either a response or an error for a single mock call.
type mockCallResult struct {
	resp *llm.ChatResponse
	err  error
}

// mockClient simulates LLM responses for testing.
type mockClient struct {
	mu        sync.Mutex
	results   []mockCallResult
	callCount int
	lastCalls []mockCall
}

type mockCall struct {
	messages  []llm.Message
	maxTokens int
}

func newMockClient() *mockClient {
	return &mockClient{}
}

func (m *mockClient) QueueResponse(resp *llm.ChatResponse) {
	m.results = append(m.results, mockCallResult{resp: resp})
}

func (m *mockClient) QueueError(err error) {
	m.results = append(m.results, mockCallResult{err: err})
}

func (m *mockClient) QueuePartial(resp *llm.ChatResponse, err error) {
	m.results = append(m.results, mockCallResult{resp: resp, err: err})
}

func (m *mockClient) Chat(_ context.Context, messages []llm.Message, _ json.RawMessage, maxTokens int) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount++
	m.lastCalls = append(m.lastCalls, mockCall{messages: messages, maxTokens: maxTokens})

	idx := m.callCount - 1
	if idx >= len(m.results) {
		return nil, fmt.Errorf("mockClient: no more responses (call %d, have %d)", m.callCount, len(m.results))
	}

	result := m.results[idx]
	return result.resp, result.err
}

func (m *mockClient) LastMessages() []llm.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.lastCalls) == 0 {
		return nil
	}
	return m.lastCalls[len(m.lastCalls)-1].messages
}

// FirstCallMessages returns the messages from the first Chat call (index 0).
func (m *mockClient) FirstCallMessages() []llm.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.lastCalls) == 0 {
		return nil
	}
	return m.lastCalls[0].messages
}

// setupAgent creates an agent with a mock client and basic tool registry for testing.
func setupAgent(t *testing.T, mc *mockClient, opts ...AgentOption) *Agent {
	t.Helper()

	// Create a temp dir for tool sandbox
	tmpDir := t.TempDir()

	// Register a simple test tool
	reg := tools.New(tmpDir)
	reg.Register("echo", "Echo back the input", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{"type": "string"},
		},
	}, func(args map[string]interface{}) (string, error) {
		if text, ok := args["text"].(string); ok {
			return text, nil
		}
		return "", fmt.Errorf("missing text")
	})

	return New(mc, reg, opts...)
}

func TestProcessPlainTextResponse(t *testing.T) {
	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "This is the final answer.",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	output, err := agent.Process(context.Background(), sess, "What is 2+2?", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify exact output
	if output != "This is the final answer." {
		t.Errorf("expected exact output %q, got %q", "This is the final answer.", output)
	}

	// Verify session structure: user message + assistant message
	if len(sess.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(sess.Messages))
	}
	if sess.Messages[0].Role != session.RoleUser || sess.Messages[0].Content != "What is 2+2?" {
		t.Errorf("message[0] expected user 'What is 2+2?', got %+v", sess.Messages[0])
	}
	if sess.Messages[1].Role != session.RoleAssistant {
		t.Errorf("message[1] expected role assistant, got %s", sess.Messages[1].Role)
	}
	if sess.Messages[1].Content != "This is the final answer." {
		t.Errorf("message[1] expected content 'This is the final answer.', got %q", sess.Messages[1].Content)
	}
	if sess.Messages[1].ToolCalls != nil {
		t.Errorf("message[1] expected nil ToolCalls, got %v", sess.Messages[1].ToolCalls)
	}
}

func TestProcessToolCallLoop(t *testing.T) {
	mc := newMockClient()

	// First call: LLM decides to call "echo"
	echoTC := llm.ToolCall{ID: "call_1", Type: "function"}
	echoTC.Function.Name = "echo"
	echoTC.Function.Arguments = `{"text":"hello world"}`
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "",
		ToolCalls: []llm.ToolCall{echoTC},
	})

	// Second call: LLM gives final answer
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "The echoed result is: hello world",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	output, err := agent.Process(context.Background(), sess, "Echo hello world", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify session structure: user, assistant(tool_call), tool, assistant(final) = 4
	if len(sess.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(sess.Messages))
	}

	// Message 0: user
	if sess.Messages[0].Role != session.RoleUser || sess.Messages[0].Content != "Echo hello world" {
		t.Errorf("message[0] expected user 'Echo hello world', got %+v", sess.Messages[0])
	}

	// Message 1: assistant with tool call
	if sess.Messages[1].Role != session.RoleAssistant {
		t.Errorf("message[1] expected role assistant, got %s", sess.Messages[1].Role)
	}
	if len(sess.Messages[1].ToolCalls) != 1 {
		t.Fatalf("message[1] expected 1 tool call, got %d", len(sess.Messages[1].ToolCalls))
	}
	if sess.Messages[1].ToolCalls[0].ID != "call_1" {
		t.Errorf("message[1] expected tool call ID 'call_1', got %q", sess.Messages[1].ToolCalls[0].ID)
	}
	if sess.Messages[1].ToolCalls[0].Function.Name != "echo" {
		t.Errorf("message[1] expected tool name 'echo', got %q", sess.Messages[1].ToolCalls[0].Function.Name)
	}
	// Verify exact tool call args
	expectedArgs := `{"text":"hello world"}`
	if sess.Messages[1].ToolCalls[0].Function.Arguments != expectedArgs {
		t.Errorf("message[1] expected args %q, got %q", expectedArgs, sess.Messages[1].ToolCalls[0].Function.Arguments)
	}

	// Message 2: tool result
	if sess.Messages[2].Role != session.RoleTool {
		t.Errorf("message[2] expected role tool, got %s", sess.Messages[2].Role)
	}
	if sess.Messages[2].Content != "hello world" {
		t.Errorf("message[2] expected content 'hello world', got %q", sess.Messages[2].Content)
	}
	if sess.Messages[2].ToolCallID != "call_1" {
		t.Errorf("message[2] expected ToolCallID 'call_1', got %q", sess.Messages[2].ToolCallID)
	}

	// Message 3: final assistant
	if sess.Messages[3].Role != session.RoleAssistant {
		t.Errorf("message[3] expected role assistant, got %s", sess.Messages[3].Role)
	}
	if sess.Messages[3].Content != "The echoed result is: hello world" {
		t.Errorf("message[3] expected content 'The echoed result is: hello world', got %q", sess.Messages[3].Content)
	}

	// Verify exact output structure
	expectedOutput := "\n[Tool Call: echo]\n[Result: hello world]\nThe echoed result is: hello world"
	if output != expectedOutput {
		t.Errorf("expected output %q, got %q", expectedOutput, output)
	}
}

func TestProcessMaxIterationsSyntheticClosing(t *testing.T) {
	mc := newMockClient()

	// LLM keeps calling tools — exceeds max_tool_iterations (3)
	for i := 0; i < 5; i++ {
		resp := &llm.ChatResponse{
			Content: "",
			ToolCalls: []llm.ToolCall{
				{ID: fmt.Sprintf("call_%d", i), Type: "function"},
			},
		}
		resp.ToolCalls[0].Function.Name = "echo"
		resp.ToolCalls[0].Function.Arguments = `{"text":"loop"}`
		mc.QueueResponse(resp)
	}

	agent := setupAgent(t, mc, WithMaxToolIterations(3))
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	output, err := agent.Process(ctx, sess, "Loop forever", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify exact message count: 1 user + 3 assistant(tool) + 3 tool results + 1 synthetic = 8
	if len(sess.Messages) != 8 {
		t.Fatalf("expected 8 messages, got %d", len(sess.Messages))
	}

	// Message 0: user
	if sess.Messages[0].Role != session.RoleUser || sess.Messages[0].Content != "Loop forever" {
		t.Errorf("message[0] mismatch: %+v", sess.Messages[0])
	}

	// Messages 1,3,5: assistant with tool calls (call_0, call_1, call_2)
	// Messages 2,4,6: tool results
	for i := 0; i < 3; i++ {
		assistantIdx := 1 + i*2
		toolIdx := 2 + i*2

		if sess.Messages[assistantIdx].Role != session.RoleAssistant {
			t.Errorf("message[%d] expected role assistant, got %s", assistantIdx, sess.Messages[assistantIdx].Role)
		}
		expectedID := fmt.Sprintf("call_%d", i)
		if len(sess.Messages[assistantIdx].ToolCalls) != 1 {
			t.Fatalf("message[%d] expected 1 tool call, got %d", assistantIdx, len(sess.Messages[assistantIdx].ToolCalls))
		}
		if sess.Messages[assistantIdx].ToolCalls[0].ID != expectedID {
			t.Errorf("message[%d] expected tool call ID %q, got %q", assistantIdx, expectedID, sess.Messages[assistantIdx].ToolCalls[0].ID)
		}
		if sess.Messages[assistantIdx].ToolCalls[0].Function.Name != "echo" {
			t.Errorf("message[%d] expected tool name 'echo', got %q", assistantIdx, sess.Messages[assistantIdx].ToolCalls[0].Function.Name)
		}

		if sess.Messages[toolIdx].Role != session.RoleTool {
			t.Errorf("message[%d] expected role tool, got %s", toolIdx, sess.Messages[toolIdx].Role)
		}
		if sess.Messages[toolIdx].ToolCallID != expectedID {
			t.Errorf("message[%d] expected ToolCallID %q, got %q", toolIdx, expectedID, sess.Messages[toolIdx].ToolCallID)
		}
	}

	// Message 7: synthetic closing assistant message
	lastMsg := sess.Messages[7]
	if lastMsg.Role != session.RoleAssistant {
		t.Errorf("message[7] expected role assistant, got %s", lastMsg.Role)
	}
	expectedContent := "I reached my tool call limit this turn. Would you like me to continue?"
	if lastMsg.Content != expectedContent {
		t.Errorf("message[7] expected content %q, got %q", expectedContent, lastMsg.Content)
	}

	// Verify exact output structure with all tool calls, results, and synthetic closing
	expectedOutput := "\n[Tool Call: echo]\n[Result: loop]\n\n[Tool Call: echo]\n[Result: loop]\n\n[Tool Call: echo]\n[Result: loop]\n\nI reached my tool call limit this turn. Would you like me to continue?"
	if output != expectedOutput {
		t.Errorf("expected output %q, got %q", expectedOutput, output)
	}
}

func TestProcessMaxIterationsNormalExitUnaffected(t *testing.T) {
	mc := newMockClient()

	// LLM calls a tool once, then gives final answer — no exhaustion
	echoTC := llm.ToolCall{ID: "call_1", Type: "function"}
	echoTC.Function.Name = "echo"
	echoTC.Function.Arguments = `{"text":"hello"}`
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "",
		ToolCalls: []llm.ToolCall{echoTC},
	})
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Done.",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	output, err := agent.Process(context.Background(), sess, "Say hello", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify exact message count: 1 user + 1 assistant(tool) + 1 tool result + 1 assistant(final) = 4
	if len(sess.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(sess.Messages))
	}

	// Message 0: user
	if sess.Messages[0].Role != session.RoleUser || sess.Messages[0].Content != "Say hello" {
		t.Errorf("message[0] mismatch: %+v", sess.Messages[0])
	}

	// Message 1: assistant with tool call
	if sess.Messages[1].Role != session.RoleAssistant {
		t.Errorf("message[1] expected role assistant, got %s", sess.Messages[1].Role)
	}
	if len(sess.Messages[1].ToolCalls) != 1 {
		t.Fatalf("message[1] expected 1 tool call, got %d", len(sess.Messages[1].ToolCalls))
	}
	if sess.Messages[1].ToolCalls[0].ID != "call_1" {
		t.Errorf("message[1] expected tool call ID 'call_1', got %q", sess.Messages[1].ToolCalls[0].ID)
	}
	if sess.Messages[1].ToolCalls[0].Function.Name != "echo" {
		t.Errorf("message[1] expected tool name 'echo', got %q", sess.Messages[1].ToolCalls[0].Function.Name)
	}

	// Message 2: tool result
	if sess.Messages[2].Role != session.RoleTool {
		t.Errorf("message[2] expected role tool, got %s", sess.Messages[2].Role)
	}
	if sess.Messages[2].Content != "hello" {
		t.Errorf("message[2] expected content 'hello', got %q", sess.Messages[2].Content)
	}
	if sess.Messages[2].ToolCallID != "call_1" {
		t.Errorf("message[2] expected ToolCallID 'call_1', got %q", sess.Messages[2].ToolCallID)
	}

	// Message 3: final assistant
	if sess.Messages[3].Role != session.RoleAssistant {
		t.Errorf("message[3] expected role assistant, got %s", sess.Messages[3].Role)
	}
	if sess.Messages[3].Content != "Done." {
		t.Errorf("message[3] expected content 'Done.', got %q", sess.Messages[3].Content)
	}
	if sess.Messages[3].ToolCalls != nil {
		t.Errorf("message[3] expected nil ToolCalls, got %v", sess.Messages[3].ToolCalls)
	}

	// Verify exact output structure
	expectedOutput := "\n[Tool Call: echo]\n[Result: hello]\nDone."
	if output != expectedOutput {
		t.Errorf("expected output %q, got %q", expectedOutput, output)
	}
}

func TestProcessToolError(t *testing.T) {
	mc := newMockClient()

	// First call: LLM calls a non-existent tool (will error)
	resp1 := &llm.ChatResponse{
		Content: "",
		ToolCalls: []llm.ToolCall{
			{ID: "call_err", Type: "function"},
		},
	}
	resp1.ToolCalls[0].Function.Name = "nonexistent_tool"
	resp1.ToolCalls[0].Function.Arguments = `{}`
	mc.QueueResponse(resp1)

	// Second call: LLM recovers
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "I apologize for the error.",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	output, err := agent.Process(context.Background(), sess, "Do something", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify exact message count: 1 user + 1 assistant(tool=nonexistent) + 1 tool(error) + 1 assistant(recovery) = 4
	if len(sess.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(sess.Messages))
	}

	// Message 0: user
	if sess.Messages[0].Role != session.RoleUser || sess.Messages[0].Content != "Do something" {
		t.Errorf("message[0] mismatch: %+v", sess.Messages[0])
	}

	// Message 1: assistant with tool call to nonexistent tool
	if sess.Messages[1].Role != session.RoleAssistant {
		t.Errorf("message[1] expected role assistant, got %s", sess.Messages[1].Role)
	}
	if len(sess.Messages[1].ToolCalls) != 1 {
		t.Fatalf("message[1] expected 1 tool call, got %d", len(sess.Messages[1].ToolCalls))
	}
	if sess.Messages[1].ToolCalls[0].ID != "call_err" {
		t.Errorf("message[1] expected tool call ID 'call_err', got %q", sess.Messages[1].ToolCalls[0].ID)
	}
	if sess.Messages[1].ToolCalls[0].Function.Name != "nonexistent_tool" {
		t.Errorf("message[1] expected tool name 'nonexistent_tool', got %q", sess.Messages[1].ToolCalls[0].Function.Name)
	}

	// Message 2: tool error result
	if sess.Messages[2].Role != session.RoleTool {
		t.Errorf("message[2] expected role tool, got %s", sess.Messages[2].Role)
	}
	if sess.Messages[2].Content != "unknown tool: nonexistent_tool" {
		t.Errorf("message[2] expected content 'unknown tool: nonexistent_tool', got %q", sess.Messages[2].Content)
	}
	if sess.Messages[2].ToolCallID != "call_err" {
		t.Errorf("message[2] expected ToolCallID 'call_err', got %q", sess.Messages[2].ToolCallID)
	}

	// Message 3: recovery assistant
	if sess.Messages[3].Role != session.RoleAssistant {
		t.Errorf("message[3] expected role assistant, got %s", sess.Messages[3].Role)
	}
	if sess.Messages[3].Content != "I apologize for the error." {
		t.Errorf("message[3] expected content 'I apologize for the error.', got %q", sess.Messages[3].Content)
	}

	// Verify exact output structure
	expectedOutput := "\n[Tool Call: nonexistent_tool]\n[Result: unknown tool: nonexistent_tool]\nI apologize for the error."
	if output != expectedOutput {
		t.Errorf("expected output %q, got %q", expectedOutput, output)
	}
}

func TestProcessLLMError(t *testing.T) {

	mc := newMockClient()
	mc.QueueError(fmt.Errorf("connection refused"))

	agent := setupAgent(t, mc)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	_, err := agent.Process(context.Background(), sess, "Hello", "You are helpful.", session.ImageAttachment{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expectedErr := "LLM call failed (iteration 0): connection refused"
	if err.Error() != expectedErr {
		t.Errorf("expected error %q, got %q", expectedErr, err.Error())
	}

	// Only user message in session — no partial response to record
	if len(sess.Messages) != 1 {
		t.Errorf("expected 1 message (user only), got %d", len(sess.Messages))
	}
}

func TestProcessPartialResponse(t *testing.T) {

	mc := newMockClient()
	mc.QueuePartial(&llm.ChatResponse{
		Content:          "This is a partial ",
		ReasoningContent: "thinking about it",
		ToolCalls:        nil,
	}, fmt.Errorf("connection reset — partial response"))

	agent := setupAgent(t, mc)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	output, err := agent.Process(context.Background(), sess, "Hello", "You are helpful.", session.ImageAttachment{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expectedErr := "LLM call interrupted (iteration 0, partial response saved): connection reset \u2014 partial response"
	if err.Error() != expectedErr {
		t.Errorf("expected error %q, got %q", expectedErr, err.Error())
	}

	// Verify exact output structure
	expectedOutput := "[Reasoning: thinking about it]\nThis is a partial "
	if output != expectedOutput {
		t.Errorf("expected output %q, got %q", expectedOutput, output)
	}

	// Session should have: user message + partial assistant message
	if len(sess.Messages) != 2 {
		t.Fatalf("expected 2 messages (user + partial assistant), got %d", len(sess.Messages))
	}
	if sess.Messages[1].Role != session.RoleAssistant {
		t.Errorf("expected assistant role, got %s", sess.Messages[1].Role)
	}
	if sess.Messages[1].Content != "This is a partial " {
		t.Errorf("expected partial content, got %q", sess.Messages[1].Content)
	}
	if sess.Messages[1].ReasoningContent != "thinking about it" {
		t.Errorf("expected partial reasoning, got %q", sess.Messages[1].ReasoningContent)
	}
}

func TestSummarizationTriggersAtThreshold(t *testing.T) {

	mc := newMockClient()

	// First call: summarization LLM call
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "## Summary\n\nTask: count to three.\nCompleted: counted to one.\n",
		ToolCalls: nil,
	})

	// Second call: main LLM call after summarization
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Short answer.",
		ToolCalls: nil,
	})

	// Very small context window (100 tokens = ~400 chars)
	agent := setupAgent(t, mc, WithContextTokens(100), WithSummarizeThreshold(0.90), WithSummarizeKeepRecent(2))
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	// Add messages that exceed the context window
	longText := strings.Repeat("x", 500)
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:    session.RoleUser,
		Content: longText,
	})
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:    session.RoleAssistant,
		Content: strings.Repeat("y", 500),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := agent.Process(ctx, sess, "New message", "System.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Session should have: summary + kept recent (2) + new user + final assistant = 4
	if len(sess.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(sess.Messages))
	}

	// Message 0: summary message
	if sess.Messages[0].Role != session.RoleAssistant {
		t.Errorf("message[0] expected role assistant, got %s", sess.Messages[0].Role)
	}
	expectedContentPrefix := "[Summary of prior conversation]"
	if !strings.HasPrefix(sess.Messages[0].Content, expectedContentPrefix) {
		t.Errorf("message[0] expected Content to start with %q, got %q", expectedContentPrefix, sess.Messages[0].Content)
	}
	if !sess.Messages[0].Summary {
		t.Error("message[0] expected Summary flag to be true")
	}

	// Message 1: kept recent assistant (old assistant, not summarized)
	if sess.Messages[1].Role != session.RoleAssistant {
		t.Errorf("message[1] expected role assistant, got %s", sess.Messages[1].Role)
	}
	if sess.Messages[1].Content != strings.Repeat("y", 500) {
		t.Errorf("message[1] expected content %q, got %q", strings.Repeat("y", 500), sess.Messages[1].Content)
	}

	// Message 2: new user message
	if sess.Messages[2].Role != session.RoleUser {
		t.Errorf("message[2] expected role user, got %s", sess.Messages[2].Role)
	}
	if sess.Messages[2].Content != "New message" {
		t.Errorf("message[2] expected content 'New message', got %q", sess.Messages[2].Content)
	}

	// Message 3: final assistant
	if sess.Messages[3].Role != session.RoleAssistant {
		t.Errorf("message[3] expected role assistant, got %s", sess.Messages[3].Role)
	}
	if sess.Messages[3].Content != "Short answer." {
		t.Errorf("message[3] expected content 'Short answer.', got %q", sess.Messages[3].Content)
	}
}

func TestSummarizationFailure(t *testing.T) {

	mc := newMockClient()

	// First call: summarization LLM call fails
	mc.QueueError(fmt.Errorf("context summarization LLM error"))

	agent := setupAgent(t, mc, WithContextTokens(100))
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	// Add messages that exceed the context window
	longText := strings.Repeat("x", 500)
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:    session.RoleUser,
		Content: longText,
	})
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:    session.RoleAssistant,
		Content: strings.Repeat("y", 500),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := agent.Process(ctx, sess, "New message", "System.", session.ImageAttachment{})
	if err == nil {
		t.Fatal("expected error from summarization failure, got nil")
	}
	expectedErr := "context summarization failed: context summarization LLM error"
	if err.Error() != expectedErr {
		t.Errorf("expected error %q, got %q", expectedErr, err.Error())
	}

	// Session should have: 2 old messages + new user message + tool error message = 4
	if len(sess.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(sess.Messages))
	}

	// Message 0: old user message
	if sess.Messages[0].Role != session.RoleUser || sess.Messages[0].Content != longText {
		t.Errorf("message[0] mismatch: %+v", sess.Messages[0])
	}

	// Message 1: old assistant message
	if sess.Messages[1].Role != session.RoleAssistant || sess.Messages[1].Content != strings.Repeat("y", 500) {
		t.Errorf("message[1] mismatch: %+v", sess.Messages[1])
	}

	// Message 2: new user message
	if sess.Messages[2].Role != session.RoleUser || sess.Messages[2].Content != "New message" {
		t.Errorf("message[2] mismatch: %+v", sess.Messages[2])
	}

	// Message 3: tool error message recording summarization failure
	if sess.Messages[3].Role != session.RoleTool {
		t.Errorf("message[3] expected role tool, got %s", sess.Messages[3].Role)
	}
	if sess.Messages[3].Content != expectedErr {
		t.Errorf("message[3] expected content %q, got %q", expectedErr, sess.Messages[3].Content)
	}
}

func TestSummarizationSkippedWhenUnderThreshold(t *testing.T) {

	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Done.",
		ToolCalls: nil,
	})

	// Large context window — summarization should not trigger
	agent := setupAgent(t, mc)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	_, err := agent.Process(context.Background(), sess, "Hello", "System.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Only one LLM call should have been made (no summarization)
	if mc.callCount != 1 {
		t.Errorf("expected 1 LLM call (no summarization), got %d", mc.callCount)
	}

	// Verify session structure: user + assistant = 2
	if len(sess.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(sess.Messages))
	}
	if sess.Messages[0].Role != session.RoleUser || sess.Messages[0].Content != "Hello" {
		t.Errorf("message[0] mismatch: %+v", sess.Messages[0])
	}
	if sess.Messages[1].Role != session.RoleAssistant {
		t.Errorf("message[1] expected role assistant, got %s", sess.Messages[1].Role)
	}
	if sess.Messages[1].Content != "Done." {
		t.Errorf("message[1] expected content 'Done.', got %q", sess.Messages[1].Content)
	}
}

func TestSplitMessages(t *testing.T) {
	msgs := make([]session.ConversationMessage, 5)
	for i := range msgs {
		msgs[i] = session.ConversationMessage{
			Role:    session.RoleUser,
			Content: fmt.Sprintf("msg-%d", i),
		}
	}

	old, recent := splitMessages(msgs, 2)

	if len(old) != 3 {
		t.Errorf("expected 3 old messages, got %d", len(old))
	}
	if len(recent) != 2 {
		t.Errorf("expected 2 recent messages, got %d", len(recent))
	}
	if recent[0].Content != "msg-3" {
		t.Errorf("expected recent[0] to be msg-3, got %s", recent[0].Content)
	}
	if recent[1].Content != "msg-4" {
		t.Errorf("expected recent[1] to be msg-4, got %s", recent[1].Content)
	}
}

func TestSplitMessagesKeepZero(t *testing.T) {
	msgs := make([]session.ConversationMessage, 3)
	for i := range msgs {
		msgs[i] = session.ConversationMessage{
			Role:    session.RoleUser,
			Content: fmt.Sprintf("msg-%d", i),
		}
	}

	old, recent := splitMessages(msgs, 0)

	if len(old) != 3 {
		t.Errorf("expected 3 old messages, got %d", len(old))
	}
	if len(recent) != 0 {
		t.Errorf("expected 0 recent messages, got %d", len(recent))
	}
}

func TestSplitMessagesKeepAll(t *testing.T) {
	msgs := make([]session.ConversationMessage, 3)
	for i := range msgs {
		msgs[i] = session.ConversationMessage{
			Role:    session.RoleUser,
			Content: fmt.Sprintf("msg-%d", i),
		}
	}

	old, recent := splitMessages(msgs, 10)

	if len(old) != 3 {
		t.Errorf("expected 3 old messages (keep >= len means all old), got %d", len(old))
	}
	if recent != nil {
		t.Errorf("expected nil recent, got %d messages", len(recent))
	}
}

func TestSystemPromptPreserved(t *testing.T) {

	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Done.",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc)
	sess := &session.Session{ChannelID: "test", Messages: nil}

	_, err := agent.Process(context.Background(), sess, "Hi", "You are a robot.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify the last LLM call included the system prompt
	lastMsgs := mc.LastMessages()
	if len(lastMsgs) == 0 {
		t.Fatal("no messages sent to LLM")
	}
	if lastMsgs[0].Role != "system" {
		t.Errorf("expected role 'system', got %q", lastMsgs[0].Role)
	}
	var sysContent string
	if err := json.Unmarshal(lastMsgs[0].Content, &sysContent); err != nil {
		t.Fatalf("unmarshal system content: %v", err)
	}
	if sysContent != "You are a robot." {
		t.Errorf("expected 'You are a robot.', got %q", sysContent)
	}
}

func TestMultipleToolCallsInOneTurn(t *testing.T) {

	mc := newMockClient()

	// First call: LLM calls echo twice
	resp1 := &llm.ChatResponse{
		Content: "",
		ToolCalls: []llm.ToolCall{
			{ID: "call_a", Type: "function"},
			{ID: "call_b", Type: "function"},
		},
	}
	resp1.ToolCalls[0].Function.Name = "echo"
	resp1.ToolCalls[0].Function.Arguments = `{"text":"first"}`
	resp1.ToolCalls[1].Function.Name = "echo"
	resp1.ToolCalls[1].Function.Arguments = `{"text":"second"}`
	mc.QueueResponse(resp1)

	// Second call: LLM finishes
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Both done.",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc)
	sess := &session.Session{ChannelID: "test", Messages: nil}

	output, err := agent.Process(context.Background(), sess, "Echo twice", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify exact output structure with both tool calls, results, and final answer
	expectedOutput := "\n[Tool Call: echo]\n[Result: first]\n\n[Tool Call: echo]\n[Result: second]\nBoth done."
	if output != expectedOutput {
		t.Errorf("expected output %q, got %q", expectedOutput, output)
	}

	// Verify session structure: user + assistant(2 tool_calls) + tool_result(echo) + tool_result(echo) + assistant(final) = 5
	if len(sess.Messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(sess.Messages))
	}

	// Message 0: user
	if sess.Messages[0].Role != session.RoleUser || sess.Messages[0].Content != "Echo twice" {
		t.Errorf("message[0] mismatch: %+v", sess.Messages[0])
	}

	// Message 1: assistant with 2 tool calls
	if sess.Messages[1].Role != session.RoleAssistant {
		t.Errorf("message[1] expected role assistant, got %s", sess.Messages[1].Role)
	}
	if len(sess.Messages[1].ToolCalls) != 2 {
		t.Fatalf("message[1] expected 2 tool calls, got %d", len(sess.Messages[1].ToolCalls))
	}
	if sess.Messages[1].ToolCalls[0].ID != "call_a" {
		t.Errorf("message[1] expected first tool call ID 'call_a', got %q", sess.Messages[1].ToolCalls[0].ID)
	}
	if sess.Messages[1].ToolCalls[1].ID != "call_b" {
		t.Errorf("message[1] expected second tool call ID 'call_b', got %q", sess.Messages[1].ToolCalls[1].ID)
	}

	// Messages 2,3: tool results
	if sess.Messages[2].Role != session.RoleTool || sess.Messages[2].Content != "first" {
		t.Errorf("message[2] mismatch: %+v", sess.Messages[2])
	}
	if sess.Messages[2].ToolCallID != "call_a" {
		t.Errorf("message[2] expected ToolCallID 'call_a', got %q", sess.Messages[2].ToolCallID)
	}
	if sess.Messages[3].Role != session.RoleTool || sess.Messages[3].Content != "second" {
		t.Errorf("message[3] mismatch: %+v", sess.Messages[3])
	}
	if sess.Messages[3].ToolCallID != "call_b" {
		t.Errorf("message[3] expected ToolCallID 'call_b', got %q", sess.Messages[3].ToolCallID)
	}

	// Message 4: final assistant
	if sess.Messages[4].Role != session.RoleAssistant {
		t.Errorf("message[4] expected role assistant, got %s", sess.Messages[4].Role)
	}
	if sess.Messages[4].Content != "Both done." {
		t.Errorf("message[4] expected content 'Both done.', got %q", sess.Messages[4].Content)
	}
}

func TestProcessEmptyContentResponse(t *testing.T) {

	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc)
	sess := &session.Session{ChannelID: "test", Messages: nil}

	output, err := agent.Process(context.Background(), sess, "Silent", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Empty content is valid — no tool calls, no text
	if output != "" {
		t.Errorf("expected empty output, got %q", output)
	}

	// Verify session structure: user + assistant (empty content, nil ToolCalls) = 2
	if len(sess.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(sess.Messages))
	}
	if sess.Messages[0].Role != session.RoleUser || sess.Messages[0].Content != "Silent" {
		t.Errorf("message[0] mismatch: %+v", sess.Messages[0])
	}
	if sess.Messages[1].Role != session.RoleAssistant {
		t.Errorf("message[1] expected role assistant, got %s", sess.Messages[1].Role)
	}
	if sess.Messages[1].Content != "" {
		t.Errorf("message[1] expected empty content, got %q", sess.Messages[1].Content)
	}
	if sess.Messages[1].ToolCalls != nil {
		t.Errorf("message[1] expected nil ToolCalls, got %v", sess.Messages[1].ToolCalls)
	}
}

func TestProcessWithImageAttachment(t *testing.T) {

	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "I see a photo of a cat.",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	att := session.ImageAttachment{Data: "iVBORw0KGgo=", MIMEType: "image/png"}
	output, err := agent.Process(context.Background(), sess, "what is this?", "You are helpful.", att)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify exact output
	if output != "I see a photo of a cat." {
		t.Errorf("expected output 'I see a photo of a cat.', got %q", output)
	}

	// User message should have the attachment
	if len(sess.Messages) < 1 {
		t.Fatal("expected at least 1 message in session")
	}
	if len(sess.Messages[0].Attachments) != 1 {
		t.Errorf("expected 1 attachment on user message, got %d", len(sess.Messages[0].Attachments))
	}
	if sess.Messages[0].Attachments[0].Data != "iVBORw0KGgo=" {
		t.Errorf("attachment data mismatch: %q", sess.Messages[0].Attachments[0].Data)
	}

	// Verify the LLM received a multimodal message
	lastMsgs := mc.LastMessages()
	if len(lastMsgs) < 2 {
		t.Fatal("expected at least 2 LLM messages (system + user)")
	}
	// User message content should be a content-parts array (json.RawMessage)
	var parts []map[string]interface{}
	if err := json.Unmarshal(lastMsgs[1].Content, &parts); err != nil {
		t.Fatalf("expected user message content to be a content-parts array, got: %s", string(lastMsgs[1].Content))
	}
	if len(parts) != 2 {
		t.Errorf("expected 2 content parts (text + image), got %d", len(parts))
	}
	if parts[0]["type"] != "text" {
		t.Errorf("expected first part type 'text', got %v", parts[0]["type"])
	}
	if parts[1]["type"] != "image_url" {
		t.Errorf("expected second part type 'image_url', got %v", parts[1]["type"])
	}
}

// --- New behavioral tests for Process() ---

func TestProcessReasoningContentRecordedInSession(t *testing.T) {
	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{
		Content:          "The answer is 42.",
		ReasoningContent: "I calculated this by adding 20 and 22.",
		ToolCalls:        nil,
	})

	agent := setupAgent(t, mc)
	sess := &session.Session{ChannelID: "test", Messages: nil}

	_, err := agent.Process(context.Background(), sess, "What is 20+22?", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Session should have: user + assistant
	if len(sess.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(sess.Messages))
	}

	// Assistant message should have ReasoningContent recorded
	assistant := sess.Messages[1]
	if assistant.Role != session.RoleAssistant {
		t.Errorf("expected role=assistant, got %s", assistant.Role)
	}
	if assistant.Content != "The answer is 42." {
		t.Errorf("expected content 'The answer is 42.', got %q", assistant.Content)
	}
	if assistant.ReasoningContent != "I calculated this by adding 20 and 22." {
		t.Errorf("expected reasoning 'I calculated this by adding 20 and 22.', got %q", assistant.ReasoningContent)
	}
	if len(assistant.ToolCalls) != 0 {
		t.Errorf("expected no tool calls, got %d", len(assistant.ToolCalls))
	}
}

func TestProcessReasoningOnlyResponse(t *testing.T) {
	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{
		Content:          "",
		ReasoningContent: "After careful analysis, the answer is 42.",
		ToolCalls:        nil,
	})

	agent := setupAgent(t, mc)
	sess := &session.Session{ChannelID: "test", Messages: nil}

	output, err := agent.Process(context.Background(), sess, "Think about it", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Output should contain reasoning with prefix
	expected := "[Reasoning: After careful analysis, the answer is 42.]\n"
	if output != expected {
		t.Errorf("expected %q, got %q", expected, output)
	}

	// Session should have assistant with reasoning but empty content
	assistant := sess.Messages[1]
	if assistant.Role != session.RoleAssistant {
		t.Errorf("expected role=assistant, got %s", assistant.Role)
	}
	if assistant.Content != "" {
		t.Errorf("expected empty content, got %q", assistant.Content)
	}
	if assistant.ReasoningContent != "After careful analysis, the answer is 42." {
		t.Errorf("expected reasoning content, got %q", assistant.ReasoningContent)
	}
}

func TestProcessReasoningOutputFormat(t *testing.T) {
	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{
		Content:          "Final answer.",
		ReasoningContent: "Step 1: think. Step 2: conclude.",
		ToolCalls:        nil,
	})

	agent := setupAgent(t, mc)
	sess := &session.Session{ChannelID: "test", Messages: nil}

	output, err := agent.Process(context.Background(), sess, "Question", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify exact output structure: reasoning prefix + content
	expectedOutput := "[Reasoning: Step 1: think. Step 2: conclude.]\nFinal answer."
	if output != expectedOutput {
		t.Errorf("expected output %q, got %q", expectedOutput, output)
	}

	// Verify session state: assistant message has reasoning content
	if len(sess.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(sess.Messages))
	}
	assistant := sess.Messages[1]
	if assistant.Role != session.RoleAssistant {
		t.Errorf("expected role assistant, got %s", assistant.Role)
	}
	if assistant.Content != "Final answer." {
		t.Errorf("expected content 'Final answer.', got %q", assistant.Content)
	}
	if assistant.ReasoningContent != "Step 1: think. Step 2: conclude." {
		t.Errorf("expected reasoning 'Step 1: think. Step 2: conclude.', got %q", assistant.ReasoningContent)
	}
	if assistant.ToolCalls != nil {
		t.Errorf("expected nil ToolCalls, got %v", assistant.ToolCalls)
	}
}

func TestProcessMultipleDifferentToolCalls(t *testing.T) {
	tmpDir := t.TempDir()
	reg := tools.New(tmpDir)

	// Register two different tools
	reg.Register("echo", "Echo back input", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{"type": "string"},
		},
	}, func(args map[string]interface{}) (string, error) {
		if text, ok := args["text"].(string); ok {
			return text, nil
		}
		return "", fmt.Errorf("missing text")
	})

	reg.Register("upper", "Convert to uppercase", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{"type": "string"},
		},
	}, func(args map[string]interface{}) (string, error) {
		if text, ok := args["text"].(string); ok {
			return strings.ToUpper(text), nil
		}
		return "", fmt.Errorf("missing text")
	})

	mc := newMockClient()

	// First call: LLM calls both echo and upper
	resp1 := &llm.ChatResponse{
		Content: "",
		ToolCalls: []llm.ToolCall{
			{ID: "call_echo", Type: "function"},
			{ID: "call_upper", Type: "function"},
		},
	}
	resp1.ToolCalls[0].Function.Name = "echo"
	resp1.ToolCalls[0].Function.Arguments = `{"text":"hello"}`
	resp1.ToolCalls[1].Function.Name = "upper"
	resp1.ToolCalls[1].Function.Arguments = `{"text":"world"}`
	mc.QueueResponse(resp1)

	// Second call: LLM gives final answer
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Echoed: hello, Uppercased: WORLD",
		ToolCalls: nil,
	})

	agent := New(mc, reg)
	sess := &session.Session{ChannelID: "test", Messages: nil}

	output, err := agent.Process(context.Background(), sess, "Use both tools", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify exact output structure with both tool calls, results, and final answer
	expectedOutput := "\n[Tool Call: echo]\n[Result: hello]\n\n[Tool Call: upper]\n[Result: WORLD]\nEchoed: hello, Uppercased: WORLD"
	if output != expectedOutput {
		t.Errorf("expected output %q, got %q", expectedOutput, output)
	}

	// Session should have: user + assistant(tool_calls) + tool_result(echo) + tool_result(upper) + assistant(final) = 5
	if len(sess.Messages) != 5 {
		t.Errorf("expected 5 messages, got %d", len(sess.Messages))
		for i, m := range sess.Messages {
			t.Logf("  msg[%d]: role=%s content=%q toolCalls=%d", i, m.Role, m.Content, len(m.ToolCalls))
		}
	}

	// Verify tool call IDs are preserved in session
	assistantWithTools := sess.Messages[1]
	if len(assistantWithTools.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls in assistant message, got %d", len(assistantWithTools.ToolCalls))
	}
	if assistantWithTools.ToolCalls[0].ID != "call_echo" {
		t.Errorf("expected first tool call ID 'call_echo', got %q", assistantWithTools.ToolCalls[0].ID)
	}
	if assistantWithTools.ToolCalls[1].ID != "call_upper" {
		t.Errorf("expected second tool call ID 'call_upper', got %q", assistantWithTools.ToolCalls[1].ID)
	}

	// Verify tool results reference the correct tool call IDs
	if sess.Messages[2].ToolCallID != "call_echo" {
		t.Errorf("expected tool result call_echo, got %q", sess.Messages[2].ToolCallID)
	}
	if sess.Messages[3].ToolCallID != "call_upper" {
		t.Errorf("expected tool result call_upper, got %q", sess.Messages[3].ToolCallID)
	}
}

func TestProcessImageAttachmentWithToolCall(t *testing.T) {
	mc := newMockClient()

	// First call: LLM sees image and calls a tool
	resp1 := &llm.ChatResponse{
		Content: "",
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Type: "function"},
		},
	}
	resp1.ToolCalls[0].Function.Name = "echo"
	resp1.ToolCalls[0].Function.Arguments = `{"text":"described"}`
	mc.QueueResponse(resp1)

	// Second call: LLM gives final answer
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "I see a cat and described it.",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc)
	sess := &session.Session{ChannelID: "test", Messages: nil}

	att := session.ImageAttachment{Data: "iVBORw0KGgo=", MIMEType: "image/png"}
	_, err := agent.Process(context.Background(), sess, "what is this?", "You are helpful.", att)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Session should have: user(attached) + assistant(tool_call) + tool + assistant(final) = 4
	if len(sess.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(sess.Messages))
	}

	// Message 0: user with attachment
	if sess.Messages[0].Role != session.RoleUser {
		t.Errorf("message[0] expected role user, got %s", sess.Messages[0].Role)
	}
	if sess.Messages[0].Content != "what is this?" {
		t.Errorf("message[0] expected content 'what is this?', got %q", sess.Messages[0].Content)
	}
	if len(sess.Messages[0].Attachments) != 1 {
		t.Errorf("message[0] expected 1 attachment, got %d", len(sess.Messages[0].Attachments))
	}

	// Message 1: assistant with tool call
	if sess.Messages[1].Role != session.RoleAssistant {
		t.Errorf("message[1] expected role assistant, got %s", sess.Messages[1].Role)
	}
	if len(sess.Messages[1].ToolCalls) != 1 {
		t.Fatalf("message[1] expected 1 tool call, got %d", len(sess.Messages[1].ToolCalls))
	}
	if sess.Messages[1].ToolCalls[0].ID != "call_1" {
		t.Errorf("message[1] expected tool call ID 'call_1', got %q", sess.Messages[1].ToolCalls[0].ID)
	}
	if sess.Messages[1].ToolCalls[0].Function.Name != "echo" {
		t.Errorf("message[1] expected tool name 'echo', got %q", sess.Messages[1].ToolCalls[0].Function.Name)
	}

	// Message 2: tool result
	if sess.Messages[2].Role != session.RoleTool {
		t.Errorf("message[2] expected role tool, got %s", sess.Messages[2].Role)
	}
	if sess.Messages[2].Content != "described" {
		t.Errorf("message[2] expected content 'described', got %q", sess.Messages[2].Content)
	}
	if sess.Messages[2].ToolCallID != "call_1" {
		t.Errorf("message[2] expected ToolCallID 'call_1', got %q", sess.Messages[2].ToolCallID)
	}

	// Message 3: final assistant
	if sess.Messages[3].Role != session.RoleAssistant {
		t.Errorf("message[3] expected role assistant, got %s", sess.Messages[3].Role)
	}
	if sess.Messages[3].Content != "I see a cat and described it." {
		t.Errorf("message[3] expected content 'I see a cat and described it.', got %q", sess.Messages[3].Content)
	}
}

func TestProcessReasoningPreservedAfterToolLoop(t *testing.T) {
	mc := newMockClient()

	// First call: LLM calls a tool, includes reasoning
	echoTC := llm.ToolCall{ID: "call_1", Type: "function"}
	echoTC.Function.Name = "echo"
	echoTC.Function.Arguments = `{"text":"hello"}`
	mc.QueueResponse(&llm.ChatResponse{
		Content:          "",
		ReasoningContent: "I should echo the input first.",
		ToolCalls:        []llm.ToolCall{echoTC},
	})

	// Second call: LLM gives final answer with reasoning
	mc.QueueResponse(&llm.ChatResponse{
		Content:          "Done.",
		ReasoningContent: "The echo was successful.",
		ToolCalls:        nil,
	})

	agent := setupAgent(t, mc)
	sess := &session.Session{ChannelID: "test", Messages: nil}

	output, err := agent.Process(context.Background(), sess, "Echo hello", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// First assistant message (with tool call) should preserve reasoning
	assistantWithTools := sess.Messages[1]
	if assistantWithTools.ReasoningContent != "I should echo the input first." {
		t.Errorf("expected reasoning preserved in tool-call assistant msg, got %q", assistantWithTools.ReasoningContent)
	}

	// Final assistant message should also preserve reasoning
	finalAssistant := sess.Messages[3]
	if finalAssistant.ReasoningContent != "The echo was successful." {
		t.Errorf("expected reasoning preserved in final assistant msg, got %q", finalAssistant.ReasoningContent)
	}
	if finalAssistant.Content != "Done." {
		t.Errorf("expected content 'Done.', got %q", finalAssistant.Content)
	}

	// Verify exact output structure with reasoning blocks, tool call, result, and final answer
	expectedOutput := "[Reasoning: I should echo the input first.]\n\n[Tool Call: echo]\n[Result: hello]\n[Reasoning: The echo was successful.]\nDone."
	if output != expectedOutput {
		t.Errorf("expected output %q, got %q", expectedOutput, output)
	}
}

// --- Tests for convertLLMToolCalls helper ---

func TestConvertLLMToolCallsEmpty(t *testing.T) {
	result := convertLLMToolCalls(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}

	empty := []llm.ToolCall{}
	result = convertLLMToolCalls(empty)
	// nil and empty slice are both acceptable for zero elements
	if result != nil && len(result) != 0 {
		t.Errorf("expected empty/nil for empty input, got %v (len=%d)", result, len(result))
	}
}

func TestConvertLLMToolCallsSingle(t *testing.T) {
	input := []llm.ToolCall{
		{ID: "call_1", Type: "function"},
	}
	input[0].Function.Name = "echo"
	input[0].Function.Arguments = `{"text":"hi"}`

	result := convertLLMToolCalls(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 element, got %d", len(result))
	}
	if result[0].ID != "call_1" {
		t.Errorf("expected ID 'call_1', got %q", result[0].ID)
	}
	if result[0].Type != "function" {
		t.Errorf("expected Type 'function', got %q", result[0].Type)
	}
	if result[0].Function.Name != "echo" {
		t.Errorf("expected Name 'echo', got %q", result[0].Function.Name)
	}
	if result[0].Function.Arguments != `{"text":"hi"}` {
		t.Errorf("expected Arguments '{\"text\":\"hi\"}', got %q", result[0].Function.Arguments)
	}
}

func TestConvertLLMToolCallsMultiple(t *testing.T) {
	input := []llm.ToolCall{
		{ID: "call_a", Type: "function"},
		{ID: "call_b", Type: "function"},
		{ID: "call_c", Type: "function"},
	}
	input[0].Function.Name = "echo"
	input[0].Function.Arguments = `{"text":"a"}`
	input[1].Function.Name = "upper"
	input[1].Function.Arguments = `{"text":"b"}`
	input[2].Function.Name = "echo"
	input[2].Function.Arguments = `{"text":"c"}`

	result := convertLLMToolCalls(input)
	if len(result) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result))
	}
	for i, expectedID := range []string{"call_a", "call_b", "call_c"} {
		if result[i].ID != expectedID {
			t.Errorf("element[%d]: expected ID %q, got %q", i, expectedID, result[i].ID)
		}
	}
}

// --- Tests for convertSessionToolCalls helper ---

func TestConvertSessionToolCallsEmpty(t *testing.T) {
	result := convertSessionToolCalls(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}

	empty := []session.ToolCall{}
	result = convertSessionToolCalls(empty)
	// nil and empty slice are both acceptable for zero elements
	if result != nil && len(result) != 0 {
		t.Errorf("expected empty/nil for empty input, got %v (len=%d)", result, len(result))
	}
}

func TestConvertSessionToolCallsSingle(t *testing.T) {
	input := []session.ToolCall{
		{ID: "call_1", Type: "function"},
	}
	input[0].Function.Name = "view"
	input[0].Function.Arguments = `{"path":"foo.md"}`

	result := convertSessionToolCalls(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 element, got %d", len(result))
	}
	if result[0].ID != "call_1" {
		t.Errorf("expected ID 'call_1', got %q", result[0].ID)
	}
	if result[0].Type != "function" {
		t.Errorf("expected Type 'function', got %q", result[0].Type)
	}
	if result[0].Function.Name != "view" {
		t.Errorf("expected Name 'view', got %q", result[0].Function.Name)
	}
	if result[0].Function.Arguments != `{"path":"foo.md"}` {
		t.Errorf("expected Arguments, got %q", result[0].Function.Arguments)
	}
}

func TestConvertSessionToolCallsMultiple(t *testing.T) {
	input := []session.ToolCall{
		{ID: "call_a", Type: "function"},
		{ID: "call_b", Type: "function"},
	}
	input[0].Function.Name = "echo"
	input[0].Function.Arguments = `{"text":"a"}`
	input[1].Function.Name = "bash"
	input[1].Function.Arguments = `{"cmd":"ls"}`

	result := convertSessionToolCalls(input)
	if len(result) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(result))
	}
	if result[0].ID != "call_a" || result[0].Function.Name != "echo" {
		t.Errorf("element[0] mismatch: %+v", result[0])
	}
	if result[1].ID != "call_b" || result[1].Function.Name != "bash" {
		t.Errorf("element[1] mismatch: %+v", result[1])
	}
}

// --- Tests for accumulateOutput helper ---

func TestAccumulateOutputReasoningOnly(t *testing.T) {
	var buf strings.Builder
	agent := setupAgent(t, newMockClient())

	resp := &llm.ChatResponse{
		Content:          "",
		ReasoningContent: "thinking hard",
		ToolCalls:        nil,
	}

	agent.accumulateOutput(resp, &buf, false, nil)

	expected := "[Reasoning: thinking hard]\n"
	if buf.String() != expected {
		t.Errorf("expected %q, got %q", expected, buf.String())
	}
}

func TestAccumulateOutputContentOnly(t *testing.T) {
	var buf strings.Builder
	agent := setupAgent(t, newMockClient())

	resp := &llm.ChatResponse{
		Content:          "answer",
		ReasoningContent: "",
		ToolCalls:        nil,
	}

	agent.accumulateOutput(resp, &buf, false, nil)

	expected := "answer"
	if buf.String() != expected {
		t.Errorf("expected %q, got %q", expected, buf.String())
	}
}

func TestAccumulateOutputBoth(t *testing.T) {
	var buf strings.Builder
	agent := setupAgent(t, newMockClient())

	resp := &llm.ChatResponse{
		Content:          "final",
		ReasoningContent: "reasoned",
		ToolCalls:        nil,
	}

	agent.accumulateOutput(resp, &buf, false, nil)

	expected := "[Reasoning: reasoned]\nfinal"
	if buf.String() != expected {
		t.Errorf("expected %q, got %q", expected, buf.String())
	}
}

func TestAccumulateOutputEmpty(t *testing.T) {
	var buf strings.Builder
	agent := setupAgent(t, newMockClient())

	resp := &llm.ChatResponse{}
	agent.accumulateOutput(resp, &buf, false, nil)

	if buf.String() != "" {
		t.Errorf("expected empty output, got %q", buf.String())
	}
}

// --- Tests for convertMessage with ToolCallID ---

func TestConvertMessageWithToolCallID(t *testing.T) {
	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{Content: "ok", ToolCalls: nil})

	agent := setupAgent(t, mc)
	sess := &session.Session{
		ChannelID: "test",
		Messages: []session.ConversationMessage{
			{
				Role:       session.RoleTool,
				Content:    "result",
				ToolCallID: "call_123",
			},
		},
	}

	_, err := agent.Process(context.Background(), sess, "continue", "System.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify the tool message was converted with ToolCallID
	lastMsgs := mc.LastMessages()
	var foundTool bool
	for _, msg := range lastMsgs {
		if msg.ToolCallID == "call_123" {
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Error("expected tool message with ToolCallID 'call_123' in LLM messages")
	}
}

// --- Tests for convertMessage with both ToolCalls and ToolCallID ---

func TestConvertMessageWithBothToolCallsAndToolCallID(t *testing.T) {
	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{Content: "ok", ToolCalls: nil})

	agent := setupAgent(t, mc)
	sess := &session.Session{
		ChannelID: "test",
		Messages: []session.ConversationMessage{
			{
				Role:             session.RoleAssistant,
				Content:          "",
				ReasoningContent: "decided",
				ToolCalls: []session.ToolCall{
					{ID: "call_x", Type: "function"},
				},
				ToolCallID: "call_prev",
			},
		},
	}

	_, err := agent.Process(context.Background(), sess, "continue", "System.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify both ToolCalls and ToolCallID are in the LLM message
	lastMsgs := mc.LastMessages()
	var found bool
	for _, msg := range lastMsgs {
		if msg.ToolCallID == "call_prev" && len(msg.ToolCalls) == 1 && msg.ToolCalls[0].ID == "call_x" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected assistant message with both ToolCalls and ToolCallID")
	}
}

// --- Tests for toMultimodalMessage with empty content ---

func TestToMultimodalMessageEmptyContent(t *testing.T) {
	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{Content: "ok", ToolCalls: nil})

	agent := setupAgent(t, mc)
	sess := &session.Session{
		ChannelID: "test",
		Messages: []session.ConversationMessage{
			{
				Role:        session.RoleUser,
				Content:     "",
				Attachments: []session.ImageAttachment{{Data: "img1", MIMEType: "image/png"}},
			},
		},
	}

	_, err := agent.Process(context.Background(), sess, "", "System.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// User message should have only the image part (no text part)
	lastMsgs := mc.LastMessages()
	var parts []map[string]interface{}
	if err := json.Unmarshal(lastMsgs[1].Content, &parts); err != nil {
		t.Fatalf("expected content-parts array, got: %s", string(lastMsgs[1].Content))
	}
	if len(parts) != 1 {
		t.Errorf("expected 1 content part (image only), got %d", len(parts))
	}
	if parts[0]["type"] != "image_url" {
		t.Errorf("expected image_url part, got %v", parts[0])
	}
}

// --- Tests for toMultimodalMessage with multiple attachments ---

func TestToMultimodalMessageMultipleAttachments(t *testing.T) {
	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{Content: "ok", ToolCalls: nil})

	agent := setupAgent(t, mc)
	sess := &session.Session{
		ChannelID: "test",
		Messages: []session.ConversationMessage{
			{
				Role:    session.RoleUser,
				Content: "compare these",
				Attachments: []session.ImageAttachment{
					{Data: "img1", MIMEType: "image/png"},
					{Data: "img2", MIMEType: "image/jpeg"},
				},
			},
		},
	}

	_, err := agent.Process(context.Background(), sess, "compare", "System.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should have text + 2 image parts = 3 parts
	lastMsgs := mc.LastMessages()
	var parts []map[string]interface{}
	if err := json.Unmarshal(lastMsgs[1].Content, &parts); err != nil {
		t.Fatalf("expected content-parts array, got: %s", string(lastMsgs[1].Content))
	}
	if len(parts) != 3 {
		t.Errorf("expected 3 content parts (text + 2 images), got %d", len(parts))
	}
	if parts[0]["type"] != "text" {
		t.Errorf("expected first part type 'text', got %v", parts[0]["type"])
	}
	if parts[1]["type"] != "image_url" || parts[2]["type"] != "image_url" {
		t.Errorf("expected image_url for parts 1 and 2, got %v, %v", parts[1]["type"], parts[2]["type"])
	}
}

// --- Tests for toMultimodalMessage with tool calls + attachments ---

func TestToMultimodalMessageToolCallsWithAttachments(t *testing.T) {
	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{Content: "ok", ToolCalls: nil})

	agent := setupAgent(t, mc)
	sess := &session.Session{
		ChannelID: "test",
		Messages: []session.ConversationMessage{
			{
				Role:    session.RoleAssistant,
				Content: "",
				ToolCalls: []session.ToolCall{
					{ID: "call_1", Type: "function"},
				},
				Attachments: []session.ImageAttachment{{Data: "img1", MIMEType: "image/png"}},
			},
		},
	}

	_, err := agent.Process(context.Background(), sess, "continue", "System.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify the message has both multimodal content and tool calls
	lastMsgs := mc.LastMessages()
	assistantMsg := lastMsgs[1]

	// Should be multimodal (JSON array content)
	var parts []map[string]interface{}
	if err := json.Unmarshal(assistantMsg.Content, &parts); err != nil {
		t.Fatalf("expected multimodal content, got: %s", string(assistantMsg.Content))
	}
	if len(parts) != 1 || parts[0]["type"] != "image_url" {
		t.Errorf("expected image_url part, got %v", parts)
	}

	// Should also have tool calls
	if len(assistantMsg.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(assistantMsg.ToolCalls))
	}
	if assistantMsg.ToolCalls[0].ID != "call_1" {
		t.Errorf("expected tool call ID 'call_1', got %q", assistantMsg.ToolCalls[0].ID)
	}
}

// --- parseToolResult tests ---

func TestParseToolResult_ValidJSONWithAttachment(t *testing.T) {
	// Valid JSON with __attachment key and text field
	input := `{"text":"done","__attachment":{"data":"base64img","mime_type":"image/png"}}`
	text, attachments := parseToolResult(input)

	if text != "done" {
		t.Errorf("expected text 'done', got %q", text)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0].Data != "base64img" {
		t.Errorf("expected attachment data 'base64img', got %q", attachments[0].Data)
	}
	if attachments[0].MIMEType != "image/png" {
		t.Errorf("expected mime_type 'image/png', got %q", attachments[0].MIMEType)
	}
}

func TestParseToolResult_ValidJSONWithoutAttachment(t *testing.T) {
	// Valid JSON without __attachment key — should return raw result
	input := `{"result":"hello","status":"ok"}`
	text, attachments := parseToolResult(input)

	if text != input {
		t.Errorf("expected raw result, got %q", text)
	}
	if attachments != nil {
		t.Errorf("expected nil attachments, got %v", attachments)
	}
}

func TestParseToolResult_NonJSONInput(t *testing.T) {
	// Non-JSON input — should return raw result unchanged
	input := `just plain text`
	text, attachments := parseToolResult(input)

	if text != input {
		t.Errorf("expected raw result %q, got %q", input, text)
	}
	if attachments != nil {
		t.Errorf("expected nil attachments, got %v", attachments)
	}
}

func TestParseToolResult_AttachmentNoText(t *testing.T) {
	// JSON with __attachment but no text field — returns "" + attachment
	input := `{"__attachment":{"data":"abc123","mime_type":"image/jpeg"}}`
	text, attachments := parseToolResult(input)

	if text != "" {
		t.Errorf("expected empty text, got %q", text)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0].Data != "abc123" {
		t.Errorf("expected attachment data 'abc123', got %q", attachments[0].Data)
	}
	if attachments[0].MIMEType != "image/jpeg" {
		t.Errorf("expected mime_type 'image/jpeg', got %q", attachments[0].MIMEType)
	}
}

func TestParseToolResult_InvalidAttachmentMarshal(t *testing.T) {
	// __attachment is a JSON array — Marshal succeeds, Unmarshal into ImageAttachment fails
	input := `{"__attachment":[1,2,3],"text":"hello"}`
	text, attachments := parseToolResult(input)

	if text != input {
		t.Errorf("expected raw result on unmarshal failure, got %q", text)
	}
	if attachments != nil {
		t.Errorf("expected nil attachments on unmarshal failure, got %v", attachments)
	}
}

func TestParseToolResult_InvalidAttachmentUnmarshal(t *testing.T) {
	// __attachment is a valid JSON string — Marshal succeeds, Unmarshal into struct fails
	input := `{"__attachment":"not an object","text":"hello"}`
	text, attachments := parseToolResult(input)

	if text != input {
		t.Errorf("expected raw result on unmarshal failure, got %q", text)
	}
	if attachments != nil {
		t.Errorf("expected nil attachments on unmarshal failure, got %v", attachments)
	}
}

func TestParseToolResult_EmptyString(t *testing.T) {
	// Empty string is not valid JSON — should return raw empty string
	text, attachments := parseToolResult("")

	if text != "" {
		t.Errorf("expected empty text, got %q", text)
	}
	if attachments != nil {
		t.Errorf("expected nil attachments, got %v", attachments)
	}
}

func TestParseToolResult_AttachmentWithExtraFields(t *testing.T) {
	// __attachment has extra fields beyond Data/MIMEType — should still extract Data and MIMEType
	input := `{"text":"ok","__attachment":{"data":"xyz","mime_type":"image/gif","extra":"ignored"}}`
	text, attachments := parseToolResult(input)

	if text != "ok" {
		t.Errorf("expected text 'ok', got %q", text)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0].Data != "xyz" {
		t.Errorf("expected data 'xyz', got %q", attachments[0].Data)
	}
	if attachments[0].MIMEType != "image/gif" {
		t.Errorf("expected mime_type 'image/gif', got %q", attachments[0].MIMEType)
	}
}

func TestParseToolResult_TextNonString(t *testing.T) {
	// text field is a number — type assertion fails, returns "" + attachment
	input := `{"text":123,"__attachment":{"data":"img","mime_type":"image/png"}}`
	text, attachments := parseToolResult(input)

	if text != "" {
		t.Errorf("expected empty text for non-string text field, got %q", text)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0].Data != "img" {
		t.Errorf("expected attachment data 'img', got %q", attachments[0].Data)
	}
}

func TestParseToolResult_AttachmentNull(t *testing.T) {
	// __attachment is JSON null — Marshal("null") succeeds, Unmarshal into struct also succeeds (zero values)
	// So it returns the text field + attachment with zero-value fields
	input := `{"__attachment":null,"text":"hello"}`
	text, attachments := parseToolResult(input)

	if text != "hello" {
		t.Errorf("expected text 'hello' (text field extracted), got %q", text)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0].Data != "" {
		t.Errorf("expected zero-data for null attachment, got %q", attachments[0].Data)
	}
	if attachments[0].MIMEType != "" {
		t.Errorf("expected zero-mime_type for null attachment, got %q", attachments[0].MIMEType)
	}
}

func TestParseToolResult_AttachmentMissingFields(t *testing.T) {
	// __attachment object exists but has no data/mime_type — zero values
	input := `{"text":"ok","__attachment":{}}`
	text, attachments := parseToolResult(input)

	if text != "ok" {
		t.Errorf("expected text 'ok', got %q", text)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0].Data != "" {
		t.Errorf("expected zero-data, got %q", attachments[0].Data)
	}
	if attachments[0].MIMEType != "" {
		t.Errorf("expected zero-mime_type, got %q", attachments[0].MIMEType)
	}
}

// --- Message conversion tests ---

func TestConvertMessage_ToolCallID(t *testing.T) {
	agent := setupAgent(t, newMockClient())

	msg := session.ConversationMessage{
		Role:       session.RoleTool,
		Content:    "tool output here",
		ToolCallID: "call_abc123",
	}

	result := agent.convertMessage(msg)

	if result.Role != "tool" {
		t.Errorf("expected role 'tool', got %q", result.Role)
	}

	var contentStr string
	if err := json.Unmarshal(result.Content, &contentStr); err != nil {
		t.Fatalf("invalid content JSON: %v", err)
	}
	if contentStr != "tool output here" {
		t.Errorf("expected content 'tool output here', got %q", contentStr)
	}

	if result.ToolCallID != "call_abc123" {
		t.Errorf("expected ToolCallID 'call_abc123', got %q", result.ToolCallID)
	}

	if result.ToolCalls != nil {
		t.Errorf("expected nil ToolCalls, got %d", len(result.ToolCalls))
	}
}

func TestConvertMessage_ToolCallsAndToolCallID(t *testing.T) {
	agent := setupAgent(t, newMockClient())

	msg := session.ConversationMessage{
		Role:       session.RoleAssistant,
		Content:    "calling a tool",
		ToolCallID: "call_xyz",
		ToolCalls: []session.ToolCall{
			{ID: "call_1", Type: "function", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "echo", Arguments: `{"text":"hi"}`}},
			{ID: "call_2", Type: "function", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "view", Arguments: `{"path":"f.md"}`}},
		},
	}

	result := agent.convertMessage(msg)

	if result.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", result.Role)
	}

	if result.ToolCallID != "call_xyz" {
		t.Errorf("expected ToolCallID 'call_xyz', got %q", result.ToolCallID)
	}

	if len(result.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(result.ToolCalls))
	}

	if result.ToolCalls[0].ID != "call_1" || result.ToolCalls[0].Function.Name != "echo" {
		t.Errorf("tool call 0: got %+v", result.ToolCalls[0])
	}
	if result.ToolCalls[1].ID != "call_2" || result.ToolCalls[1].Function.Name != "view" {
		t.Errorf("tool call 1: got %+v", result.ToolCalls[1])
	}
}

func TestConvertMessage_PureText(t *testing.T) {
	agent := setupAgent(t, newMockClient())

	msg := session.ConversationMessage{
		Role:             session.RoleUser,
		Content:          "Hello world",
		ReasoningContent: "thinking silently",
	}

	result := agent.convertMessage(msg)

	if result.Role != "user" {
		t.Errorf("expected role 'user', got %q", result.Role)
	}

	var contentStr string
	if err := json.Unmarshal(result.Content, &contentStr); err != nil {
		t.Fatalf("invalid content JSON: %v", err)
	}
	if contentStr != "Hello world" {
		t.Errorf("expected content 'Hello world', got %q", contentStr)
	}

	if result.ReasoningContent != "thinking silently" {
		t.Errorf("expected reasoning 'thinking silently', got %q", result.ReasoningContent)
	}

	if result.ToolCallID != "" {
		t.Errorf("expected empty ToolCallID, got %q", result.ToolCallID)
	}
	if result.ToolCalls != nil {
		t.Errorf("expected nil ToolCalls, got %d", len(result.ToolCalls))
	}
}

func TestConvertMessage_Empty(t *testing.T) {
	agent := setupAgent(t, newMockClient())

	msg := session.ConversationMessage{
		Role: session.RoleUser,
	}

	result := agent.convertMessage(msg)

	if result.Role != "user" {
		t.Errorf("expected role 'user', got %q", result.Role)
	}

	var contentStr string
	if err := json.Unmarshal(result.Content, &contentStr); err != nil {
		t.Fatalf("invalid content JSON: %v", err)
	}
	if contentStr != "" {
		t.Errorf("expected empty content, got %q", contentStr)
	}
}

func TestToMultimodalMessage_ImageOnly(t *testing.T) {
	agent := setupAgent(t, newMockClient())

	msg := session.ConversationMessage{
		Role: session.RoleUser,
		Attachments: []session.ImageAttachment{
			{Data: "aW1hZ2VkYXRh", MIMEType: "image/png"},
		},
	}

	result := agent.toMultimodalMessage(msg)

	if result.Role != "user" {
		t.Errorf("expected role 'user', got %q", result.Role)
	}

	var parts []map[string]interface{}
	if err := json.Unmarshal(result.Content, &parts); err != nil {
		t.Fatalf("invalid content JSON: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}

	if parts[0]["type"] != "image_url" {
		t.Errorf("expected part type 'image_url', got %q", parts[0]["type"])
	}

	imgURL, ok := parts[0]["image_url"].(map[string]interface{})
	if !ok {
		t.Fatal("expected image_url to be a map")
	}
	if url, ok := imgURL["url"].(string); !ok || !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("expected data URI prefix, got %q", url)
	}
}

func TestToMultimodalMessage_MultipleAttachments(t *testing.T) {
	agent := setupAgent(t, newMockClient())

	msg := session.ConversationMessage{
		Role:    session.RoleUser,
		Content: "look at these",
		Attachments: []session.ImageAttachment{
			{Data: "aW1hZ2Ux", MIMEType: "image/png"},
			{Data: "aW1hZ2Uy", MIMEType: "image/jpeg"},
			{Data: "aW1hZ2Uz", MIMEType: "image/webp"},
		},
	}

	result := agent.toMultimodalMessage(msg)

	var parts []map[string]interface{}
	if err := json.Unmarshal(result.Content, &parts); err != nil {
		t.Fatalf("invalid content JSON: %v", err)
	}

	// 1 text part + 3 image parts = 4
	if len(parts) != 4 {
		t.Fatalf("expected 4 parts, got %d", len(parts))
	}

	// First part should be text
	if parts[0]["type"] != "text" || parts[0]["text"] != "look at these" {
		t.Errorf("expected text part, got %+v", parts[0])
	}

	// Remaining parts should be image_url
	for i := 1; i <= 3; i++ {
		if parts[i]["type"] != "image_url" {
			t.Errorf("part %d: expected image_url, got %q", i, parts[i]["type"])
		}
	}
}

func TestToMultimodalMessage_ToolCallsAndAttachments(t *testing.T) {
	agent := setupAgent(t, newMockClient())

	msg := session.ConversationMessage{
		Role:    session.RoleAssistant,
		Content: "I see the image",
		Attachments: []session.ImageAttachment{
			{Data: "aW1hZ2VkYXRh", MIMEType: "image/png"},
		},
		ToolCalls: []session.ToolCall{
			{ID: "call_1", Type: "function", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "view", Arguments: `{"path":"img.png"}`}},
		},
		ToolCallID: "call_prev",
	}

	result := agent.toMultimodalMessage(msg)

	// Verify multimodal content
	var parts []map[string]interface{}
	if err := json.Unmarshal(result.Content, &parts); err != nil {
		t.Fatalf("invalid content JSON: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (text + image), got %d", len(parts))
	}
	if parts[0]["type"] != "text" || parts[0]["text"] != "I see the image" {
		t.Errorf("text part mismatch: %+v", parts[0])
	}
	if parts[1]["type"] != "image_url" {
		t.Errorf("expected image_url part, got %q", parts[1]["type"])
	}

	// Verify tool call metadata applied
	if result.ToolCallID != "call_prev" {
		t.Errorf("expected ToolCallID 'call_prev', got %q", result.ToolCallID)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ID != "call_1" || result.ToolCalls[0].Function.Name != "view" {
		t.Errorf("tool call mismatch: %+v", result.ToolCalls[0])
	}
}

func TestApplyToolCalls_NoToolCallsOrID(t *testing.T) {
	msg := session.ConversationMessage{
		Role:    session.RoleUser,
		Content: "no tools here",
	}

	llmMsg := llm.NewTextMessage("user", msg.Content)
	applyToolCalls(&llmMsg, msg)

	if llmMsg.ToolCallID != "" {
		t.Errorf("expected empty ToolCallID, got %q", llmMsg.ToolCallID)
	}
	if llmMsg.ToolCalls != nil {
		t.Errorf("expected nil ToolCalls, got %d", len(llmMsg.ToolCalls))
	}
}

func TestApplyToolCalls_OnlyToolCallID(t *testing.T) {
	msg := session.ConversationMessage{
		Role:       session.RoleTool,
		Content:    "result",
		ToolCallID: "call_id_only",
	}

	llmMsg := llm.NewTextMessage("tool", msg.Content)
	applyToolCalls(&llmMsg, msg)

	if llmMsg.ToolCallID != "call_id_only" {
		t.Errorf("expected ToolCallID 'call_id_only', got %q", llmMsg.ToolCallID)
	}
	if llmMsg.ToolCalls != nil {
		t.Errorf("expected nil ToolCalls, got %d", len(llmMsg.ToolCalls))
	}
}

func TestApplyToolCalls_EmptyToolCallsSlice(t *testing.T) {
	msg := session.ConversationMessage{
		Role:       session.RoleAssistant,
		Content:    "empty calls",
		ToolCalls:  []session.ToolCall{},
		ToolCallID: "call_id",
	}

	llmMsg := llm.NewTextMessage("assistant", msg.Content)
	applyToolCalls(&llmMsg, msg)

	if llmMsg.ToolCallID != "call_id" {
		t.Errorf("expected ToolCallID 'call_id', got %q", llmMsg.ToolCallID)
	}
	if llmMsg.ToolCalls != nil {
		t.Errorf("expected nil ToolCalls for empty slice, got %d", len(llmMsg.ToolCalls))
	}
}
