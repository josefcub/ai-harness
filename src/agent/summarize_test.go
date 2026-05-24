package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/agent-project/harness/channellog"
	"github.com/agent-project/harness/llm"
	"github.com/agent-project/harness/session"
	"github.com/agent-project/harness/tools"
)

// setupAgentWithChannelLogger creates an agent with a real channelLogger pointing to a temp dir.
func setupAgentWithChannelLogger(t *testing.T, mc *mockClient, ctxTok int, sumThreshold float64, sumKeepRecent, maxToolIter, maxTok int, logTools, logReasoning bool, logDir string) *Agent {
	t.Helper()
	tmpDir := t.TempDir()
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
	chLogger := channellog.New(logDir)
	return New(mc, reg, maxToolIter, ctxTok, sumThreshold, sumKeepRecent, maxTok, SummaryPrompt, logTools, logReasoning, chLogger, nil)
}

// buildLongSession creates a session with many messages to exceed the summarization threshold.
func buildLongSession(channelID string, msgCount int, contentLen int) *session.Session {
	sess := &session.Session{
		ChannelID: channelID,
		Messages:  make([]session.ConversationMessage, 0, msgCount),
	}
	for i := 0; i < msgCount; i++ {
		role := session.RoleUser
		if i%2 == 1 {
			role = session.RoleAssistant
		}
		content := fmt.Sprintf("This is message number %d with enough content to push the token count over the limit. ", i)
		for len(content) < contentLen {
			content += content
		}
		content = content[:contentLen]
		sess.Messages = append(sess.Messages, session.ConversationMessage{
			Role:    role,
			Content: content,
		})
	}
	return sess
}

// readChannelLog reads the JSONL log file for a channel and returns the lines.
func readChannelLog(t *testing.T, logDir, channelID string) []string {
	t.Helper()
	// channellog.SanitizeFilename is used internally; we need to match.
	safeID := strings.ReplaceAll(channelID, ":", "_")
	logPath := logDir + "/" + safeID + ".log"
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read channel log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// Filter empty lines
	var result []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}

// TestSummarizeContext_EmptyContentFallbackToReasoningContent verifies that when the LLM
// returns an empty Content but non-empty ReasoningContent, the summary uses ReasoningContent.
func TestSummarizeContext_EmptyContentFallbackToReasoningContent(t *testing.T) {
	mc := newMockClient()
	logDir := t.TempDir()

	// First LLM call: summarization returns empty Content but has ReasoningContent
	mc.QueueResponse(&llm.ChatResponse{
		Content:          "",
		ReasoningContent: "The conversation discusses a project plan with milestones and deadlines.",
	})
	// Second LLM call: final answer
	mc.QueueResponse(&llm.ChatResponse{
		Content: "Here is the summary of our discussion.",
	})

	agent := setupAgentWithChannelLogger(t, mc, 8192, 0.8, 10, 20, 4096, false, false, logDir)

	// Build a session with enough messages to trigger summarization
	sess := buildLongSession("test-channel", 100, 200)

	_, err := agent.Process(context.Background(), sess, "Continue the project", "System prompt.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify the summary message was created with ReasoningContent as the summary text
	if len(sess.Messages) < 2 {
		t.Fatalf("expected at least 2 messages after summarization, got %d", len(sess.Messages))
	}

	summaryMsg := sess.Messages[0]
	if summaryMsg.Role != session.RoleAssistant {
		t.Errorf("summary message role = %s, want %s", summaryMsg.Role, session.RoleAssistant)
	}
	if summaryMsg.Content != "" {
		t.Errorf("summary message Content = %q, want empty string", summaryMsg.Content)
	}
	expectedPrefix := "[Summary of prior conversation]\nThe conversation discusses a project plan with milestones and deadlines."
	if summaryMsg.ReasoningContent != expectedPrefix {
		t.Errorf("summary message ReasoningContent = %q, want %q", summaryMsg.ReasoningContent, expectedPrefix)
	}
}

// TestSummarizeContext_MessageStructure verifies the exact structure of the summary
// message: Role=Assistant, Content="", ReasoningContent="[Summary of prior conversation]\n<text>".
func TestSummarizeContext_MessageStructure(t *testing.T) {
	mc := newMockClient()
	logDir := t.TempDir()

	mc.QueueResponse(&llm.ChatResponse{
		Content:   "The team agreed on a three-phase rollout plan.",
		ToolCalls: nil,
	})
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Done.",
		ToolCalls: nil,
	})

	agent := setupAgentWithChannelLogger(t, mc, 8192, 0.8, 10, 20, 4096, false, false, logDir)

	sess := buildLongSession("test-channel", 100, 200)

	_, err := agent.Process(context.Background(), sess, "Summarize", "System prompt.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// The first message should be the summary
	if len(sess.Messages) == 0 {
		t.Fatal("expected at least one message in session")
	}

	summaryMsg := sess.Messages[0]

	// Deep verification of summary message structure
	if summaryMsg.Role != session.RoleAssistant {
		t.Errorf("summary Role = %q, want %q", summaryMsg.Role, session.RoleAssistant)
	}
	if summaryMsg.Content != "" {
		t.Errorf("summary Content = %q, want empty string", summaryMsg.Content)
	}
	expectedPrefix := "[Summary of prior conversation]\n"
	if !strings.HasPrefix(summaryMsg.ReasoningContent, expectedPrefix) {
		t.Errorf("summary ReasoningContent missing prefix: got %q", summaryMsg.ReasoningContent)
	}
	// Verify there is actual summary text after the prefix
	summaryText := strings.TrimPrefix(summaryMsg.ReasoningContent, expectedPrefix)
	if summaryText == "" {
		t.Error("summary ReasoningContent has no text after prefix")
	}
	if len(summaryMsg.ToolCalls) > 0 {
		t.Errorf("summary message should not have ToolCalls, got %d", len(summaryMsg.ToolCalls))
	}
	if summaryMsg.ToolCallID != "" {
		t.Errorf("summary message ToolCallID = %q, want empty", summaryMsg.ToolCallID)
	}
	if len(summaryMsg.Attachments) > 0 {
		t.Errorf("summary message should not have Attachments, got %d", len(summaryMsg.Attachments))
	}
}

// TestSummarizeContext_AttachmentProtectedMessages verifies that messages with image
// attachments in the "old" region are moved to the recent region and preserved.
func TestSummarizeContext_AttachmentProtectedMessages(t *testing.T) {
	mc := newMockClient()
	logDir := t.TempDir()

	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Old messages summarized.",
		ToolCalls: nil,
	})
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Done.",
		ToolCalls: nil,
	})

	agent := setupAgentWithChannelLogger(t, mc, 8192, 0.8, 10, 20, 4096, false, false, logDir)

	// Build a session with 100 messages, where message #5 (in the "old" region) has an attachment.
	// keepRecent=10, so recentStart = 90. Message #5 is in old region (index 0-89).
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  make([]session.ConversationMessage, 100),
	}
	for i := 0; i < 100; i++ {
		role := session.RoleUser
		if i%2 == 1 {
			role = session.RoleAssistant
		}
		content := fmt.Sprintf("Message %d with enough content to push the token count over the limit. This is a longer message to ensure we exceed the threshold for context summarization. ", i)
		for len(content) < 200 {
			content += content
		}
		content = content[:200]
		msg := session.ConversationMessage{
			Role:    role,
			Content: content,
		}
		// Add attachment to message #5 (in the old region, since keepRecent=10 means recentStart=90)
		if i == 5 {
			msg.Attachments = []session.ImageAttachment{
				{
					Data:     "iVBORw0KGgoAAAANSUhEUg==",
					MIMEType: "image/png",
				},
			}
		}
		sess.Messages[i] = msg
	}

	_, err := agent.Process(context.Background(), sess, "Continue", "System prompt.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// After summarization: summary message + recent messages (10 recent + 1 protected)
	// The protected message (index 5) should be in the recent section
	if len(sess.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(sess.Messages))
	}

	// First message should be the summary
	summaryMsg := sess.Messages[0]
	if summaryMsg.Role != session.RoleAssistant {
		t.Errorf("summary Role = %q, want %q", summaryMsg.Role, session.RoleAssistant)
	}
	if summaryMsg.Content != "" {
		t.Errorf("summary Content = %q, want empty", summaryMsg.Content)
	}
	if !strings.HasPrefix(summaryMsg.ReasoningContent, "[Summary of prior conversation]\n") {
		t.Errorf("summary ReasoningContent missing prefix: %q", summaryMsg.ReasoningContent)
	}

	// Verify the protected message (with attachment) is preserved in recent messages
	hasAttachmentMsg := false
	for i := 1; i < len(sess.Messages); i++ {
		if len(sess.Messages[i].Attachments) > 0 {
			hasAttachmentMsg = true
			if sess.Messages[i].Attachments[0].MIMEType != "image/png" {
				t.Errorf("protected message attachment MIMEType = %q, want %q",
					sess.Messages[i].Attachments[0].MIMEType, "image/png")
			}
			// The protected message should have been from index 5 in the original session
			if sess.Messages[i].Content == "" {
				t.Error("protected message should have original content")
			}
			break
		}
	}
	if !hasAttachmentMsg {
		t.Error("expected protected message with attachment to be preserved in recent section")
	}
}

// TestSummarizeContext_ChannelLoggerEntries verifies that channelLogger.Log is called
// at the correct points: start, and completion. It also verifies that the LLM is
// called for summarization (mock call count >= 2) and that the session is modified.
func TestSummarizeContext_ChannelLoggerEntries(t *testing.T) {
	mc := newMockClient()
	logDir := t.TempDir()

	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Summarized.",
		ToolCalls: nil,
	})
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Done.",
		ToolCalls: nil,
	})

	agent := setupAgentWithChannelLogger(t, mc, 8192, 0.8, 10, 20, 4096, false, false, logDir)

	sess := buildLongSession("testchannel", 100, 200)

	_, err := agent.Process(context.Background(), sess, "Continue", "System prompt.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify the LLM was called at least twice: once for summarization, once for final answer
	if mc.callCount < 2 {
		t.Fatalf("expected at least 2 LLM calls (summarization + answer), got %d", mc.callCount)
	}

	// Verify the session was modified by summarization
	// Original: 100 messages + 1 user message = 101
	// After: summary message + user message + assistant message = 3 (no tool calls in this test)
	if len(sess.Messages) < 2 {
		t.Fatalf("expected at least 2 messages after summarization, got %d", len(sess.Messages))
	}

	// First message should be the summary
	summaryMsg := sess.Messages[0]
	if summaryMsg.Role != session.RoleAssistant {
		t.Errorf("summary Role = %q, want %q", summaryMsg.Role, session.RoleAssistant)
	}
	if summaryMsg.Content != "" {
		t.Errorf("summary Content = %q, want empty", summaryMsg.Content)
	}
	if !strings.HasPrefix(summaryMsg.ReasoningContent, "[Summary of prior conversation]\n") {
		t.Errorf("summary ReasoningContent missing prefix: %q", summaryMsg.ReasoningContent)
	}

	// Verify the channel log file exists and has the expected entries
	entries := readChannelLog(t, logDir, "testchannel")
	if len(entries) < 4 {
		t.Errorf("expected at least 4 channel log entries (user, summary start, summary complete, assistant), got %d", len(entries))
	}

	// Verify summarization entries are present
	hasStart := false
	hasComplete := false
	for _, entry := range entries {
		if strings.Contains(entry, "summarization started") {
			hasStart = true
		}
		if strings.Contains(entry, "summarization complete") {
			hasComplete = true
		}
	}
	if !hasStart {
		t.Error("expected 'summarization started' entry in channel log")
	}
	if !hasComplete {
		t.Error("expected 'summarization complete' entry in channel log")
	}
}

// TestSummarizeContext_UsesProductionPrompt verifies that the summarization LLM call
// actually receives SummaryPrompt (embedded from summary.md) as the system message.
// This confirms the prompt injection path from main.go → agent.New → summarizeContext.
func TestSummarizeContext_UsesProductionPrompt(t *testing.T) {
	mc := newMockClient()
	logDir := t.TempDir()

	mc.QueueResponse(&llm.ChatResponse{Content: "Summary text."})
	mc.QueueResponse(&llm.ChatResponse{Content: "Done."})

	agent := setupAgentWithChannelLogger(t, mc, 8192, 0.8, 10, 20, 4096, false, false, logDir)

	sess := buildLongSession("testchannel", 100, 200)

	_, err := agent.Process(context.Background(), sess, "Continue", "System prompt.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// The first LLM call should be the summarization call.
	// Verify its first message (the system prompt) matches SummaryPrompt.
	firstCall := mc.FirstCallMessages()
	if len(firstCall) == 0 {
		t.Fatal("expected at least one message in first LLM call")
	}

	systemMsg := firstCall[0]
	if systemMsg.Role != "system" {
		t.Errorf("first message role = %q, want %q", systemMsg.Role, "system")
	}
	// Content is stored as JSON-encoded string (json.RawMessage), so unmarshal to compare.
	var contentStr string
	if err := json.Unmarshal(systemMsg.Content, &contentStr); err != nil {
		t.Fatalf("failed to unmarshal message content: %v", err)
	}
	if contentStr != SummaryPrompt {
		t.Errorf("first message content does not match SummaryPrompt\nexpected:\n%s\n\ngot:\n%s", SummaryPrompt, contentStr)
	}

	// Verify there are additional messages (the old conversation messages) after the system prompt.
	if len(firstCall) < 2 {
		t.Errorf("expected at least 2 messages in summarization call (system + old msgs), got %d", len(firstCall))
	}
}
