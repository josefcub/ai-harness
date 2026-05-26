package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/agent-project/harness/agent"
	"github.com/agent-project/harness/llm"
	"github.com/agent-project/harness/queue"
	"github.com/agent-project/harness/sandbox"
	"github.com/agent-project/harness/session"
	"github.com/agent-project/harness/tools"
	"github.com/agent-project/harness/webhook"
)

// TestIntegration_AgentWithContextTrimming tests that the agent trims context
// when the conversation exceeds 90% of context_tokens.
func TestIntegration_AgentWithContextTrimming(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}

	workingDir, err := sandbox.ResolveWorkingDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	reg := tools.New(workingDir)
	tools.RegisterFileTools(reg)

	// Mock LLM
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`
		fmt.Fprintln(w, chunk)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer llmServer.Close()

	llmClient := llm.New(llmServer.URL, "test", "", 5*time.Second, filepath.Join(dir, "logs"), nil)
	agt := agent.New(llmClient, reg, agent.WithMaxToolIterations(3), agent.WithContextTokens(100), agent.WithSummarizeThreshold(0.90), agent.WithSummarizeKeepRecent(2))

	// Create session with many messages to exceed context
	sess := &session.Session{
		ChannelID: "test",
		Messages:  make([]session.ConversationMessage, 0, 50),
	}

	// Add enough messages to exceed context (100 tokens / 4 chars = ~25 chars limit)
	for i := 0; i < 50; i++ {
		sess.Messages = append(sess.Messages, session.ConversationMessage{
			Role:    session.RoleUser,
			Content: "this is a long message that takes up some space in the context window",
		})
		sess.Messages = append(sess.Messages, session.ConversationMessage{
			Role:    session.RoleAssistant,
			Content: "this is a response message that also takes up space",
		})
	}

	ctx := context.Background()
	_, err = agt.Process(ctx, sess, "final message", "system prompt", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	// After processing, messages should have been trimmed
	// We can't easily check exact count, but it should be well under 50
	if len(sess.Messages) > 50 {
		t.Errorf("messages should have been trimmed, got %d", len(sess.Messages))
	}
}

// TestIntegration_CallbackFailure tests that callback failures are handled gracefully.
func TestIntegration_CallbackFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}

	// Test callback to non-existent URL
	err := webhook.SendCallback("test-channel", "test message", "http://localhost:59999/callback", nil)
	if err == nil {
		t.Error("expected error for unreachable callback URL")
	}

	// Test callback to server returning 500
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badServer.Close()

	err = webhook.SendCallback("test-channel", "test message", badServer.URL, nil)
	if err == nil {
		t.Error("expected error for 500 callback response")
	}

	// Test successful callback
	goodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer goodServer.Close()

	err = webhook.SendCallback("test-channel", "test message", goodServer.URL, nil)
	if err != nil {
		t.Errorf("unexpected error for successful callback: %v", err)
	}
}

// TestIntegration_SystemPromptNotInSession tests that the system prompt
// is not stored in session files.
func TestIntegration_SystemPromptNotInSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	workingDir, err := sandbox.ResolveWorkingDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	reg := tools.New(workingDir)
	tools.RegisterFileTools(reg)

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`
		fmt.Fprintln(w, chunk)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer llmServer.Close()

	llmClient := llm.New(llmServer.URL, "test", "", 5*time.Second, filepath.Join(dir, "logs"), nil)
	agt := agent.New(llmClient, reg, agent.WithMaxToolIterations(3))

	sessMgr := session.NewManager(filepath.Join(dir, "state"))
	sess := sessMgr.Get("prompt-test")

	ctx := context.Background()
	_, err = agt.Process(ctx, sess, "test", "secret system prompt", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	// Check session messages for system prompt
	for _, m := range sess.Messages {
		if m.Content == "secret system prompt" {
			t.Error("system prompt should not be in session messages")
		}
	}

	// Save and check the file
	if err := sessMgr.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "state", "prompt-test.json"))
	if err != nil {
		t.Fatalf("read session: %v", err)
	}

	if bytes.Contains(data, []byte("secret system prompt")) {
		t.Error("system prompt should not be in session file")
	}
}

// TestIntegration_ConcurrentWebhookRequests tests that multiple concurrent
// webhook requests are handled correctly.
func TestIntegration_ConcurrentWebhookRequests(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}

	q := queue.New(100, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))

	// Simulate concurrent webhook handler behavior
	var wg sync.WaitGroup
	numRequests := 20

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch := fmt.Sprintf("ch-%d", i)
			// Create session (webhook handler does this)
			sessions.Get(ch)

			// Enqueue message
			msg := queue.Message{
				ChannelID:   ch,
				MessageText: fmt.Sprintf("message-%d", i),
				CallbackURL: "",
			}
			q.Enqueue(msg)
		}(i)
	}

	wg.Wait()

	// Verify all messages were enqueued
	if q.Len() != numRequests {
		t.Errorf("expected %d messages, got %d", numRequests, q.Len())
	}

	// Verify all sessions were created
	if sessions.Count() != numRequests {
		t.Errorf("expected %d sessions, got %d", numRequests, sessions.Count())
	}

	// Dequeue all and verify
	for i := 0; i < numRequests; i++ {
		q.Dequeue()
	}
	if q.Len() != 0 {
		t.Error("queue should be empty")
	}
}
