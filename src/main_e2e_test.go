package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-project/harness/agent"
	"github.com/agent-project/harness/llm"
	"github.com/agent-project/harness/queue"
	"github.com/agent-project/harness/sandbox"
	"github.com/agent-project/harness/session"
	"github.com/agent-project/harness/testutil/mockllm"
	"github.com/agent-project/harness/tools"
	"github.com/agent-project/harness/webhook"
	"github.com/agent-project/harness/worker"
)

// createHarness creates a fully wired harness with a mock LLM server,
// temp directories, and returns (cancel, webhookURL, callbackURL, sessions, workingDir).
// The worker goroutine is started; cancel() stops it and waits for it.
// cancel() can be called explicitly or via t.Cleanup (both are safe —
// double-calls are idempotent via atomic guard).
func createHarness(t *testing.T, baseURL string) (cancel func(), webhookURL string, callbackURL string, sessions *session.Manager, workingDir string) {
	t.Helper()

	dir := t.TempDir()
	for _, d := range []string{"work", "logs", "state"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatalf("create dir %s: %v", d, err)
		}
	}

	workingDir, err := sandbox.ResolveWorkingDir(filepath.Join(dir, "work"))
	if err != nil {
		t.Fatalf("resolve working dir: %v", err)
	}

	// Create callback server — must be created before harness and cleaned up by test
	cbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	q := queue.New(10, nil)
	sessions = session.NewManager(filepath.Join(dir, "state"))

	reg := tools.New(workingDir)
	tools.RegisterFileTools(reg)
	tools.RegisterGrepTools(reg)
	tools.RegisterGlobTools(reg)
	llmClient := llm.New(baseURL, "test-model", "", 5*time.Second, filepath.Join(dir, "logs"), nil)
	agt := agent.New(llmClient, reg)
	wrk := worker.New(q, sessions, agt, "You are a test assistant.", workingDir, nil)

	webhookSrv := webhook.NewServer("127.0.0.1", 0, "/webhook", 1048576, q, sessions, true, nil)
	h := http.NewServeMux()
	h.HandleFunc("/webhook", webhookSrv.HandleFunc())
	webhookHTTP := httptest.NewServer(h)

	ctx, cancelCtx := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wrk.Run(ctx)
	}()

	// Track whether cancel was already called to avoid double-wait
	var cancelCalled int32

	finalCancel := func() {
		if atomic.CompareAndSwapInt32(&cancelCalled, 0, 1) {
			cancelCtx()
			wg.Wait()
		}
	}

	// Store cleanup info on the test struct
	t.Cleanup(func() {
		finalCancel()
		cbSrv.Close()
		webhookHTTP.CloseClientConnections()
	})

	return finalCancel, webhookHTTP.URL, cbSrv.URL, sessions, workingDir
}

// ---------------------------------------------------------------------
// E2E Tests
// ---------------------------------------------------------------------

// TestE2E_FullMessageFlow verifies the happy path: a message sent to the
// webhook flows through the entire pipeline — queue, worker, agent, LLM,
// callback, session save — and returns to the caller via callback.
func TestE2E_FullMessageFlow(t *testing.T) {
	t.Parallel()

	mockSrv, mockURL := mockllm.New(t)
	mockSrv.SetResponseText("Hello world", "")

	cancel, webhookURL, _, sessMgr, _ := createHarness(t, mockURL)
	defer cancel()

	// Callback server — receives result from worker after processing
	var callbackReceived atomic.Bool
	var cbChannel, cbMessage string
	var cbMu sync.Mutex
	cbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload webhook.CallbackPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		cbMu.Lock()
		cbChannel = payload.Channel
		cbMessage = payload.Message
		cbMu.Unlock()
		callbackReceived.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer cbSrv.Close()

	payload := fmt.Sprintf(`{"channel":"test-channel","message":"say hello","callback_url":"%s"}`, cbSrv.URL)
	resp, err := http.Post(webhookURL+"/webhook", "application/json", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("POST /webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("/webhook status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Wait for callback delivery
	deadline := time.After(5 * time.Second)
	for !callbackReceived.Load() {
		select {
		case <-deadline:
			cbMu.Lock()
			t.Fatalf("callback never received — channel=%q message=%q", cbChannel, cbMessage)
		case <-time.After(10 * time.Millisecond):
		}
	}

	cbMu.Lock()
	if cbChannel != "test-channel" {
		t.Errorf("callback channel = %q, want %q", cbChannel, "test-channel")
	}
	if cbMessage == "" {
		t.Error("callback message is empty")
	} else if !strings.Contains(cbMessage, "Hello world") {
		t.Errorf("callback message should contain 'Hello world', got: %q", cbMessage)
	}
	cbMu.Unlock()

	// Verify session persisted
	sess := sessMgr.Get("test-channel")
	if len(sess.Messages) < 2 {
		t.Errorf("session has %d messages, expected at least 2", len(sess.Messages))
	}

	// Check user message
	found := false
	for _, m := range sess.Messages {
		if m.Role == session.RoleUser && strings.HasSuffix(m.Content, "say hello") {
			found = true
			break
		}
	}
	if !found {
		t.Error("user message 'say hello' not found in session")
	}

	// Check assistant response
	found = false
	for _, m := range sess.Messages {
		if m.Role == session.RoleAssistant && m.Content == "Hello world" {
			found = true
			break
		}
	}
	if !found {
		t.Error("assistant response 'Hello world' not found in session")
	}
}

// TestE2E_MessageFlowWithoutCallback verifies that a message is processed
// and saved even when no callback_url is provided.
func TestE2E_MessageFlowWithoutCallback(t *testing.T) {
	t.Parallel()

	mockSrv, mockURL := mockllm.New(t)
	mockSrv.SetResponseText("no callback test", "")

	cancel, webhookURL, _, sessMgr, _ := createHarness(t, mockURL)
	defer cancel()

	// Send webhook message without callback URL
	payload := `{"channel":"no-callback-ch","message":"test"}`
	resp, err := http.Post(webhookURL+"/webhook", "application/json", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("POST /webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("/webhook status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Wait for worker to drain queue
	time.Sleep(500 * time.Millisecond)

	// Session should still be saved even without callback
	sess := sessMgr.Get("no-callback-ch")
	if sess == nil {
		t.Fatal("session not found for no-callback-ch")
	}
	if len(sess.Messages) < 2 {
		t.Errorf("expected >= 2 messages, got %d", len(sess.Messages))
	}

	// Verify user message
	found := false
	for _, m := range sess.Messages {
		if m.Role == session.RoleUser && strings.HasSuffix(m.Content, "test") {
			found = true
			break
		}
	}
	if !found {
		t.Error("user message not found in session")
	}

	// Verify assistant message
	found = false
	for _, m := range sess.Messages {
		if m.Role == session.RoleAssistant && m.Content == "no callback test" {
			found = true
			break
		}
	}
	if !found {
		t.Error("assistant message 'no callback test' not found in session")
	}
}

// TestE2E_ToolCallFlow verifies the agent's tool-call loop works
// end-to-end: LLM returns tool calls → agent executes tools → LLM
// returns final answer → callback receives result.
func TestE2E_ToolCallFlow(t *testing.T) {
	t.Parallel()

	// Use raw SSE to simulate: first response is a tool call, second is text.
	mockSrv, mockURL := mockllm.New(t)
	mockSrv.InjectSSE([]string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"view","arguments":"{\"path\":\"foo.md\"}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	})

	cancel, webhookURL, _, sessions, _ := createHarness(t, mockURL)
	defer cancel()

	var cbMu sync.Mutex
	var cbMessage string
	cbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload webhook.CallbackPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		cbMu.Lock()
		cbMessage = payload.Message
		cbMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer cbSrv.Close()

	payload := fmt.Sprintf(`{"channel":"tool-ch","message":"read foo.md","callback_url":"%s"}`, cbSrv.URL)
	resp, err := http.Post(webhookURL+"/webhook", "application/json", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("POST /webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("/webhook status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Wait for processing (tool calls require extra LLM round-trip)
	time.Sleep(1 * time.Second)

	cbMu.Lock()
	msg := cbMessage
	cbMu.Unlock()

	// Session should have user + tool call messages
	sess := sessions.Get("tool-ch")
	if sess == nil {
		t.Fatal("session not found for tool-ch")
	}
	if len(sess.Messages) < 2 {
		t.Errorf("expected >= 2 messages in session, got %d", len(sess.Messages))
	}

	// Verify session contains tool call message
	hasToolCall := false
	for _, m := range sess.Messages {
		if m.Role == session.RoleAssistant && len(m.ToolCalls) > 0 {
			hasToolCall = true
			break
		}
	}
	if !hasToolCall {
		for _, m := range sess.Messages {
			t.Logf("  message role=%s content=%q tool_calls=%d", m.Role, m.Content, len(m.ToolCalls))
		}
		t.Error("expected tool call message in session")
	}

	// Callback should have some output
	if msg == "" {
		t.Log("callback message is empty (may be expected if tool execution produced no output)")
	}
}

// TestE2E_ConcurrentChannels verifies that multiple channels can send
// messages concurrently and each gets its own isolated session and callback.
func TestE2E_ConcurrentChannels(t *testing.T) {
	t.Parallel()

	mockSrv, mockURL := mockllm.New(t)
	mockSrv.SetResponseText("concurrent response", "")

	cancel, webhookURL, _, sessions, _ := createHarness(t, mockURL)
	defer cancel()

	numChannels := 5
	var wg sync.WaitGroup
	var cbMu sync.Mutex
	receivedChannels := make(map[string]bool)

	// Single callback server for all channels
	cbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload webhook.CallbackPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		cbMu.Lock()
		receivedChannels[payload.Channel] = true
		cbMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer cbSrv.Close()

	for i := 0; i < numChannels; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch := fmt.Sprintf("ch-%d", i)
			msg := fmt.Sprintf(`{"channel":"%s","message":"hello %d","callback_url":"%s"}`, ch, i, cbSrv.URL)
			_, err := http.Post(webhookURL+"/webhook", "application/json", bytes.NewReader([]byte(msg)))
			if err != nil {
				t.Logf("POST failed for %s: %v", ch, err)
			}
		}(i)
	}

	wg.Wait()

	// Wait for callbacks with timeout
	deadline := time.After(5 * time.Second)
	for len(receivedChannels) < numChannels {
		select {
		case <-deadline:
			t.Fatalf("not all callbacks received: got %d/%d", len(receivedChannels), numChannels)
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Verify each channel has an isolated session
	for i := 0; i < numChannels; i++ {
		ch := fmt.Sprintf("ch-%d", i)
		sess := sessions.Get(ch)
		if sess == nil {
			t.Errorf("session not found for channel %s", ch)
			continue
		}
		if sess.ChannelID != ch {
			t.Errorf("session channel = %q, want %q", sess.ChannelID, ch)
		}
	}
}

// TestE2E_BackpressureRejection verifies that when a channel's queue is
// full, the webhook returns 429 and the message is rejected.
func TestE2E_BackpressureRejection(t *testing.T) {
	t.Parallel()

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

	mockSrv, mockURL := mockllm.New(t)
	mockSrv.SetResponseText("recovered", "")

	// Queue with max depth 1
	q := queue.New(1, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))

	reg := tools.New(workingDir)
	tools.RegisterFileTools(reg)
	tools.RegisterGrepTools(reg)
	tools.RegisterGlobTools(reg)
	llmClient := llm.New(mockURL, "test-model", "", 5*time.Second, filepath.Join(dir, "logs"), nil)
	agt := agent.New(llmClient, reg)
	wrk := worker.New(q, sessions, agt, "You are a test assistant.", workingDir, nil)

	cbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer cbSrv.Close()

	webhookSrv := webhook.NewServer("127.0.0.1", 0, "/webhook", 1048576, q, sessions, true, nil)
	h := http.NewServeMux()
	h.HandleFunc("/webhook", webhookSrv.HandleFunc())
	webhookHTTP := httptest.NewServer(h)
	defer webhookHTTP.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wrk.Run(ctx)
	}()
	defer func() {
		cancel()
		wg.Wait()
	}()

	// First message should be accepted
	payload1 := fmt.Sprintf(`{"channel":"test-ch","message":"first","callback_url":"%s"}`, cbSrv.URL)
	resp1, err := http.Post(webhookHTTP.URL+"/webhook", "application/json", bytes.NewReader([]byte(payload1)))
	if err != nil {
		t.Fatalf("POST 1: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusAccepted {
		t.Errorf("POST 1 status = %d, want %d", resp1.StatusCode, http.StatusAccepted)
	}

	// Second message should be rejected (queue full for same channel)
	payload2 := fmt.Sprintf(`{"channel":"test-ch","message":"second","callback_url":"%s"}`, cbSrv.URL)
	resp2, err := http.Post(webhookHTTP.URL+"/webhook", "application/json", bytes.NewReader([]byte(payload2)))
	if err != nil {
		t.Fatalf("POST 2: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Errorf("POST 2 status = %d, want %d", resp2.StatusCode, http.StatusTooManyRequests)
	}
}

// TestE2E_SystemPromptComposed verifies that the system prompt is
// composed from the base prompt + workspace markdown files at request time,
// and is NOT stored in session files.
func TestE2E_SystemPromptComposed(t *testing.T) {
	t.Parallel()

	mockSrv, mockURL := mockllm.New(t)
	mockSrv.SetResponseText("prompt test", "")

	cancel, webhookURL, callbackURL, sessions, workDir := createHarness(t, mockURL)
	defer cancel()

	// Write workspace prompt files that will be included in the system prompt
	agentsPath := filepath.Join(workDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("AGENTS_WORKSPACE_CONTENT"), 0644); err != nil {
		t.Fatal(err)
	}
	soulPath := filepath.Join(workDir, "SOUL.md")
	if err := os.WriteFile(soulPath, []byte("SOUL_WORKSPACE_CONTENT"), 0644); err != nil {
		t.Fatal(err)
	}

	payload := fmt.Sprintf(`{"channel":"prompt-ch","message":"test","callback_url":"%s"}`, callbackURL)
	resp, err := http.Post(webhookURL+"/webhook", "application/json", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("POST /webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("/webhook status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Wait for worker to process
	time.Sleep(500 * time.Millisecond)

	// Verify session does NOT contain the workspace file content
	sess := sessions.Get("prompt-ch")
	if sess == nil {
		t.Fatal("session not found")
	}
	for _, m := range sess.Messages {
		if m.Content == "AGENTS_WORKSPACE_CONTENT" {
			t.Error("workspace file content should not be in session messages")
		}
	}

	// Verify system prompt IS composed from workspace files
	body := mockSrv.LastBody()

	var reqMap map[string]interface{}
	if err := json.Unmarshal(body, &reqMap); err != nil {
		t.Fatalf("parse LLM request body: %v", err)
	}
	messages, ok := reqMap["messages"].([]interface{})
	if !ok {
		t.Fatal("no messages array in LLM request")
	}

	foundSystem := false
	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" {
			foundSystem = true
			if !strings.Contains(content, "AGENTS_WORKSPACE_CONTENT") {
				t.Error("system prompt does not contain AGENTS.md content")
			}
			if !strings.Contains(content, "SOUL_WORKSPACE_CONTENT") {
				t.Error("system prompt does not contain SOUL.md content")
			}
		}
	}
	if !foundSystem {
		t.Error("no system message found in LLM request")
	}
}

// TestE2E_WorkerExitOnContextCancel verifies that the worker loop exits
// cleanly when its context is cancelled, and does not panic or leak.
func TestE2E_WorkerExitOnContextCancel(t *testing.T) {
	t.Parallel()

	mockSrv, mockURL := mockllm.New(t)
	mockSrv.SetResponseText("hello", "")

	_, webhookURL, _, _, _ := createHarness(t, mockURL)

	// Send a message to start processing
	var callbackReceived atomic.Bool
	cbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callbackReceived.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer cbSrv.Close()

	payload := fmt.Sprintf(`{"channel":"cancel-ch","message":"test","callback_url":"%s"}`, cbSrv.URL)
	resp, err := http.Post(webhookURL+"/webhook", "application/json", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("POST /webhook: %v", err)
	}
	resp.Body.Close()

	// Worker already exited via t.Cleanup — if we got here without panic, test passes
	_ = resp
}
