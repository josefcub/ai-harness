package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agent-project/harness/agent"
	"github.com/agent-project/harness/log"
	"github.com/agent-project/harness/llm"
	"github.com/agent-project/harness/queue"
	"github.com/agent-project/harness/sandbox"
	"github.com/agent-project/harness/session"
	"github.com/agent-project/harness/testutil/mockllm"
	"github.com/agent-project/harness/tools"
	"github.com/agent-project/harness/webhook"
	"github.com/agent-project/harness/worker"
)

// createHarnessForShutdown builds a fully wired harness and returns the components
// for test use. The test is responsible for starting the worker goroutine
// and cleaning up the webhook server.
func createHarnessForShutdown(t *testing.T) (
	q *queue.Queue,
	sessions *session.Manager,
	ws *webhook.Server,
	webhookHTTP *httptest.Server,
	dir string,
) {
	t.Helper()

	dir = t.TempDir()
	for _, d := range []string{"work", "logs", "state"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatalf("create dir %s: %v", d, err)
		}
	}

	workingDir, err := sandbox.ResolveWorkingDir(filepath.Join(dir, "work"))
	if err != nil {
		t.Fatalf("resolve working dir: %v", err)
	}

	q = queue.New(10, nil)
	sessions = session.NewManager(filepath.Join(dir, "state"))

	reg := tools.New(workingDir)
	tools.RegisterFileTools(reg)
	tools.RegisterGrepTools(reg)
	tools.RegisterGlobTools(reg)

	webhookSrv := webhook.NewServer("127.0.0.1", 0, "/webhook", 1048576, q, sessions, true, nil)

	// Use httptest with custom mux so we can verify 503 behavior
	customMux := http.NewServeMux()
	customMux.HandleFunc("/webhook", webhookSrv.HandleFunc())
	webhookHTTP = httptest.NewServer(customMux)

	return q, sessions, webhookSrv, webhookHTTP, dir
}

// ---------------------------------------------------------------------
// Shutdown Tests
// ---------------------------------------------------------------------

// TestShutdown_WebhookStopsAccepting verifies that after ws.Stop(),
// the webhook server returns 503 for new requests.
func TestShutdown_WebhookStopsAccepting(t *testing.T) {
	t.Parallel()

	_, _, ws, webhookHTTP, _ := createHarnessForShutdown(t)
	defer webhookHTTP.Close()

	// Initial request should succeed
	payload := `{"channel":"test","message":"hello"}`
	resp, err := http.Post(webhookHTTP.URL+"/webhook", "application/json", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("POST before stop: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("before stop: status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Stop the webhook server
	if err := ws.Stop(); err != nil {
		t.Fatalf("ws.Stop(): %v", err)
	}

	// Request after stop should return 503
	resp, err = http.Post(webhookHTTP.URL+"/webhook", "application/json", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("POST after stop: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("after stop: status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

// TestShutdown_DrainPreservesMessages verifies that drainPending +
// sessions.SaveAll preserves all pending messages in session files
// without calling LLM.
func TestShutdown_DrainPreservesMessages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, d := range []string{"logs", "state"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}

	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))

	// Enqueue 2 messages to different channels
	q.Enqueue(queue.Message{
		ChannelID:   "slack:aaa",
		MessageText: "message aaa",
	})
	q.Enqueue(queue.Message{
		ChannelID:   "slack:bbb",
		MessageText: "message bbb",
	})

	// Drain and save
	drainPending(q, sessions, logger)
	if err := sessions.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	// Verify session files exist with correct messages
	for _, ch := range []string{"slack:aaa", "slack:bbb"} {
		s := sessions.Get(ch)
		if s == nil {
			t.Fatalf("session not found for %s", ch)
		}
		if len(s.Messages) != 1 {
			t.Errorf("session %s: expected 1 message, got %d", ch, len(s.Messages))
			continue
		}
		if s.Messages[0].Role != session.RoleUser {
			t.Errorf("session %s: role = %q, want %q", ch, s.Messages[0].Role, session.RoleUser)
		}
	}

	sAaa := sessions.Get("slack:aaa")
	if sAaa.Messages[0].Content != "message aaa" {
		t.Errorf("slack:aaa content = %q, want %q", sAaa.Messages[0].Content, "message aaa")
	}

	sBbb := sessions.Get("slack:bbb")
	if sBbb.Messages[0].Content != "message bbb" {
		t.Errorf("slack:bbb content = %q, want %q", sBbb.Messages[0].Content, "message bbb")
	}
}

// TestShutdown_DrainWithExistingSessions verifies that draining appends
// to existing sessions (not replaces), preserving prior conversation history.
func TestShutdown_DrainWithExistingSessions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, d := range []string{"logs", "state"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}

	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))

	// Pre-populate session with 2 messages
	s := sessions.Get("slack:existing")
	s.Messages = append(s.Messages,
		session.ConversationMessage{Role: session.RoleUser, Content: "existing message 1"},
		session.ConversationMessage{Role: session.RoleUser, Content: "existing message 2"},
	)

	// Enqueue a pending message for the same channel
	q.Enqueue(queue.Message{
		ChannelID:   "slack:existing",
		MessageText: "drained message",
	})

	// Drain
	count := drainPending(q, sessions, logger)
	if count != 1 {
		t.Fatalf("expected 1 drained message, got %d", count)
	}

	// Save
	if err := sessions.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	// Verify session has 3 messages (2 original + 1 drained)
	s = sessions.Get("slack:existing")
	if len(s.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(s.Messages))
	}
	if s.Messages[0].Content != "existing message 1" {
		t.Errorf("message[0] = %q, want %q", s.Messages[0].Content, "existing message 1")
	}
	if s.Messages[1].Content != "existing message 2" {
		t.Errorf("message[1] = %q, want %q", s.Messages[1].Content, "existing message 2")
	}
	if s.Messages[2].Content != "drained message" {
		t.Errorf("message[2] = %q, want %q", s.Messages[2].Content, "drained message")
	}

	// Verify the file persists correctly
	data, err := os.ReadFile(filepath.Join(dir, "state", "slack_existing.json"))
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	var loaded session.Session
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("parse session: %v", err)
	}
	if len(loaded.Messages) != 3 {
		t.Errorf("persisted session has %d messages, want 3", len(loaded.Messages))
	}
}

// TestShutdown_DrainPreservesAttachments verifies that draining preserves
// image attachments on pending messages.
func TestShutdown_DrainPreservesAttachments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, d := range []string{"logs", "state"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}

	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))

	q.Enqueue(queue.Message{
		ChannelID:   "slack:img",
		MessageText: "look at this",
		ImageAttachment: session.ImageAttachment{
			Data:     "base64data",
			MIMEType: "image/png",
		},
	})

	// Drain and save
	drainPending(q, sessions, logger)
	if err := sessions.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	s := sessions.Get("slack:img")
	if s == nil {
		t.Fatal("session not found for slack:img")
	}
	if len(s.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(s.Messages))
	}

	msg := s.Messages[0]
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}
	if msg.Attachments[0].Data != "base64data" {
		t.Errorf("attachment data = %q, want %q", msg.Attachments[0].Data, "base64data")
	}
	if msg.Attachments[0].MIMEType != "image/png" {
		t.Errorf("attachment MIME type = %q, want %q", msg.Attachments[0].MIMEType, "image/png")
	}

	// Verify persistence
	data, err := os.ReadFile(filepath.Join(dir, "state", "slack_img.json"))
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	var loaded session.Session
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("parse session: %v", err)
	}
	if len(loaded.Messages[0].Attachments) != 1 {
		t.Errorf("persisted: expected 1 attachment, got %d", len(loaded.Messages[0].Attachments))
	}
}

// TestShutdown_Sequence verifies the full 5-step shutdown sequence
// completes without error: stop webhook → cancel → drain → flush sessions → clear queue.
func TestShutdown_Sequence(t *testing.T) {
	t.Parallel()

	mockSrv, mockURL := mockllm.New(t)
	mockSrv.SetResponseText("shutdown test", "")

	dir := t.TempDir()
	for _, d := range []string{"work", "logs", "state"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}

	workingDir, err := sandbox.ResolveWorkingDir(filepath.Join(dir, "work"))
	if err != nil {
		t.Fatal(err)
	}

	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))

	reg := tools.New(workingDir)
	tools.RegisterFileTools(reg)
	tools.RegisterGrepTools(reg)
	tools.RegisterGlobTools(reg)
	llmClient := llm.New(mockURL, "test-model", "", 5*time.Second, filepath.Join(dir, "logs"), nil)
	agt := agent.New(llmClient, reg)
	wrk := worker.New(q, sessions, agt, "You are a test assistant.", workingDir, logger)

	webhookSrv := webhook.NewServer("127.0.0.1", 0, "/webhook", 1048576, q, sessions, true, logger)

	customMux := http.NewServeMux()
	customMux.HandleFunc("/webhook", webhookSrv.HandleFunc())
	webhookHTTP := httptest.NewServer(customMux)
	defer webhookHTTP.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wrk.Run(ctx)
	}()

	// Enqueue a message via webhook
	payload := `{"channel":"shutdown-test","message":"hello"}`
	resp, err := http.Post(webhookHTTP.URL+"/webhook", "application/json", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("POST /webhook: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Let the worker pick up the message
	time.Sleep(200 * time.Millisecond)

	// Step 1: Stop webhook server
	if err := webhookSrv.Stop(); err != nil {
		t.Fatal("webhookSrv.Stop():", err)
	}

	// Step 2: Cancel context
	cancel()

	// Step 3: Wait for worker
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Worker exited cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit within 2s of context cancel")
	}

	// Step 4: Drain and flush
	drainPending(q, sessions, logger)
	if err := sessions.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	// Step 5: Clear queue
	q.Clear()

	// Verify results
	// Webhook is down (already verified by Stop() succeeding)

	// Queue is empty
	if q.Len() != 0 {
		t.Errorf("queue len = %d, want 0", q.Len())
	}

	// Session file exists with the user message
	sess := sessions.Get("shutdown-test")
	if sess == nil {
		t.Fatal("session not found for shutdown-test")
	}

	// Verify the file on disk
	data, err := os.ReadFile(filepath.Join(dir, "state", "shutdown_test.json"))
	if err != nil {
		// File name uses SanitizeFilename which replaces ':' with '_'
		data, err = os.ReadFile(filepath.Join(dir, "state", "shutdown-test.json"))
		if err != nil {
			// Try listing the state dir
			entries, _ := os.ReadDir(filepath.Join(dir, "state"))
			for _, e := range entries {
				t.Logf("state file: %s", e.Name())
			}
			t.Fatalf("read session file: %v", err)
		}
	}

	var loaded session.Session
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("parse session: %v", err)
	}

	if loaded.ChannelID != "shutdown-test" {
		t.Errorf("channel = %q, want %q", loaded.ChannelID, "shutdown-test")
	}

	// Message should be present
	found := false
	for _, m := range loaded.Messages {
		if m.Role == session.RoleUser && strings.Contains(m.Content, "hello") {
			found = true
			break
		}
	}
	if !found {
		t.Error("user message not found in persisted session")
	}

	if loaded.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if loaded.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}
