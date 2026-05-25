package agent

import (
	"testing"

	"github.com/agent-project/harness/session"
	"github.com/agent-project/harness/tools"
)

// setupTotalTokensAgent creates a minimal agent for testing totalTokens().
func setupTotalTokensAgent(t *testing.T) *Agent {
	t.Helper()
	tmpDir := t.TempDir()
	reg := tools.New(tmpDir)
	return New(nil, reg)
}

func TestTotalTokens_SystemPromptEstimation(t *testing.T) {
	a := setupTotalTokensAgent(t)

	// System prompt tokens: len("hello") / 3 = 1
	sess := &session.Session{ChannelID: "ch", Messages: nil}
	got := a.totalTokens(sess, "hello")
	if got != 1 {
		t.Errorf("expected system prompt tokens = 1, got %d", got)
	}

	// Empty system prompt
	got = a.totalTokens(sess, "")
	if got != 0 {
		t.Errorf("expected empty system prompt tokens = 0, got %d", got)
	}

	// 3-char prompt: 3/3 = 1
	got = a.totalTokens(sess, "abc")
	if got != 1 {
		t.Errorf("expected 3-char prompt tokens = 1, got %d", got)
	}

	// 2-char prompt: 2/3 = 0 (truncation)
	got = a.totalTokens(sess, "ab")
	if got != 0 {
		t.Errorf("expected 2-char prompt tokens = 0, got %d", got)
	}
}

func TestTotalTokens_MessageContentTokens(t *testing.T) {
	a := setupTotalTokensAgent(t)

	// 6-char content: 6/3 = 2
	sess := &session.Session{
		ChannelID: "ch",
		Messages: []session.ConversationMessage{
			{Role: "user", Content: "hello world"}, // 11 chars / 3 = 3
		},
	}
	got := a.totalTokens(sess, "sys") // 3/3 = 1
	expected := 1 + 3                 // sys + "hello world"
	if got != expected {
		t.Errorf("expected %d tokens, got %d", expected, got)
	}

	// Empty content: 0/3 = 0
	sess.Messages[0].Content = ""
	got = a.totalTokens(sess, "sys")
	if got != 1 {
		t.Errorf("expected 1 (only system prompt), got %d", got)
	}
}

func TestTotalTokens_ToolCallTokens(t *testing.T) {
	a := setupTotalTokensAgent(t)

	// Tool call: name "echo" (4/2=2) + args '{"a":1}' (5/2=2) = 4
	sess := &session.Session{
		ChannelID: "ch",
		Messages: []session.ConversationMessage{
			{
				Role:    "assistant",
				Content: "",
				ToolCalls: []session.ToolCall{
					{
						ID: "call_1",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      "echo",
							Arguments: `{"a":1}`,
						},
					},
				},
			},
		},
	}
	got := a.totalTokens(sess, "")
	// name "echo" = 4/2 = 2, args '{"a":1}' = 7/2 = 3 => total 5
	expected := 5
	if got != expected {
		t.Errorf("expected %d tool call tokens, got %d", expected, got)
	}

	// Multiple tool calls
	sess.Messages[0].ToolCalls = append(sess.Messages[0].ToolCalls, session.ToolCall{
		ID: "call_2",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      "view",
			Arguments: `{"path":"foo.md"}`,
		},
	})
	// "view" = 4/2=2, '{"path":"foo.md"}' = 17/2=8 => +10 more
	expected = 5 + 10
	if got := a.totalTokens(sess, ""); got != expected {
		t.Errorf("expected %d tokens for 2 tool calls, got %d", expected, got)
	}

	// Empty tool call name/args: ""/2 = 0
	sess.Messages[0].ToolCalls[0].Function.Name = ""
	sess.Messages[0].ToolCalls[0].Function.Arguments = ""
	got = a.totalTokens(sess, "")
	expected = 10 // only second tool call
	if got != expected {
		t.Errorf("expected %d tokens with empty first tool call, got %d", expected, got)
	}
}

func TestTotalTokens_ReasoningContentCounted(t *testing.T) {
	a := setupTotalTokensAgent(t)

	// ReasoningContent IS counted (preserve_thinking includes it in context)
	sess := &session.Session{
		ChannelID: "ch",
		Messages: []session.ConversationMessage{
			{
				Role:             "assistant",
				Content:          "answer",                                                     // 6/3 = 2
				ReasoningContent: "this is a very long reasoning block that should be counted", // 58/3 = 19
			},
		},
	}
	got := a.totalTokens(sess, "")
	// "answer"=2 + reasoning=58/3=19 => 21
	expected := 2 + 19
	if got != expected {
		t.Errorf("expected %d tokens (reasoning counted), got %d", expected, got)
	}
}

func TestTotalTokens_ToolCallIDTokens(t *testing.T) {
	a := setupTotalTokensAgent(t)

	// Content "result" = 6/3 = 2, ToolCallID "call_123" = 8/2 = 4 => total 6
	sess := &session.Session{
		ChannelID: "ch",
		Messages: []session.ConversationMessage{
			{Role: "tool", Content: "result", ToolCallID: "call_123"},
		},
	}
	got := a.totalTokens(sess, "")
	expected := 6 // content "result"=2 + ToolCallID "call_123"=4
	if got != expected {
		t.Errorf("expected %d ToolCallID tokens, got %d", expected, got)
	}

	// Empty ToolCallID: ""/2 = 0, content "result" still = 2
	sess.Messages[0].ToolCallID = ""
	got = a.totalTokens(sess, "")
	if got != 2 {
		t.Errorf("expected 2 for empty ToolCallID (only content), got %d", got)
	}
}

func TestTotalTokens_AttachmentTokens(t *testing.T) {
	a := setupTotalTokensAgent(t)

	// Each attachment = 1000 tokens
	sess := &session.Session{
		ChannelID: "ch",
		Messages: []session.ConversationMessage{
			{
				Role:        "user",
				Content:     "look at this",
				Attachments: []session.ImageAttachment{{}, {}, {}}, // 3 attachments
			},
		},
	}
	got := a.totalTokens(sess, "")
	// "look at this" = 12/3 = 4, 3 attachments * 1000 = 3000
	expected := 4 + 3000
	if got != expected {
		t.Errorf("expected %d tokens with attachments, got %d", expected, got)
	}

	// No attachments: 0
	sess.Messages[0].Attachments = nil
	got = a.totalTokens(sess, "")
	expected = 4
	if got != expected {
		t.Errorf("expected %d tokens without attachments, got %d", expected, got)
	}

	// Empty attachments slice (not nil): len(nil slice) = 0
	sess.Messages[0].Attachments = []session.ImageAttachment{}
	got = a.totalTokens(sess, "")
	if got != expected {
		t.Errorf("expected %d tokens with empty slice, got %d", expected, got)
	}
}

func TestTotalTokens_Combined(t *testing.T) {
	a := setupTotalTokensAgent(t)

	// Full scenario: system prompt + message with content, tool calls, ToolCallID, attachments
	sess := &session.Session{
		ChannelID: "ch",
		Messages: []session.ConversationMessage{
			{
				Role:    "assistant",
				Content: "thinking", // 8/3 = 2
				ToolCalls: []session.ToolCall{
					{
						ID: "call_1",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      "view",                // 4/2 = 2
							Arguments: `{"path":"file.txt"}`, // 19/2 = 9
						},
					},
				},
				Attachments: []session.ImageAttachment{{}}, // 1000
			},
			{
				Role:       "tool",
				Content:    "file contents here", // 18/3 = 6
				ToolCallID: "call_1",             // 6/2 = 3
			},
		},
	}
	got := a.totalTokens(sess, "sys") // 3/3 = 1
	// sys=1 + thinking=2 + view=2 + args=9 + attach=1000 + file contents=6 + call_1=3 = 1023
	expected := 1 + 2 + 2 + 9 + 1000 + 6 + 3
	if got != expected {
		t.Errorf("expected %d combined tokens, got %d", expected, got)
	}
}
