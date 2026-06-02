package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------- fixtures ----------

// mockSubstrateCfg controls the behavior of the mock substrate server.
type mockSubstrateCfg struct {
	// Message is the content the callback will carry.
	Message string
	// Reasoning is the reasoning content included in the callback.
	Reasoning string
	// PostCallback, when true, causes the substrate to post a callback.
	// When false, the substrate returns 200 but never sends a callback
	// (useful for testing timeout behavior).
	PostCallback bool
	// Delay is how long the substrate handler waits before responding.
	Delay time.Duration
}

// newMockSubstrate returns a test server that mimics the Substrate webhook.
// When cfg.PostCallback is true, it posts a callback echoing the channel
// from the substrate request with the configured message content.
// When cfg.PostCallback is false, it returns 200 but never sends a callback.
// The optional delay lets callers simulate processing latency.
func newMockSubstrate(t *testing.T, cfg mockSubstrateCfg) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SubstrateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("malformed substrate request: %v", err)
		}
		if cfg.Delay > 0 {
			time.Sleep(cfg.Delay)
		}
		if cfg.PostCallback {
			cb := SubstrateCallback{
				Channel:   req.Channel,
				Message:   cfg.Message,
				Reasoning: cfg.Reasoning,
			}
			body, _ := json.Marshal(cb)
			go http.Post(req.CallbackURL, "application/json", bytes.NewReader(body))
		}
		w.WriteHeader(http.StatusOK)
	}))
}

// newTestBridge builds a Bridge wired to a local test server that
// exposes both /v1/chat/completions and /callback.  Returns the
// *Bridge and the test server (caller must Close).
func newTestBridge(t *testing.T, substrateURL string) (*Bridge, *httptest.Server) {
	t.Helper()
	b := NewBridge(substrateURL)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", b.HandleCompletion)
	mux.HandleFunc("/callback", b.HandleCallback)
	ts := httptest.NewServer(mux)
	b.SetCallbackBaseURL(ts.URL)
	return b, ts
}

// doCompletion sends a JSON OpenAI request and returns the parsed response
// and raw body.  The caller controls request construction.
func doCompletion(t *testing.T, baseURL string, req OpenAIRequest) (OpenAIResponse, string) {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp, err := http.Post(baseURL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, string(raw))
	}

	var openResp OpenAIResponse
	if err := json.Unmarshal(raw, &openResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return openResp, string(raw)
}

// ---------- tests ----------

func TestBridge_MalformedPayload(t *testing.T) {
	_, ts := newTestBridge(t, "http://unused")
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json",
		bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 status for malformed JSON")
	}
}

func TestBridge_Completion(t *testing.T) {
	table := []struct {
		name     string
		callback SubstrateCallback
		assert   func(t *testing.T, resp OpenAIResponse)
	}{
		{
			name:     "returns complete message",
			callback: SubstrateCallback{Message: "Hello from mock substrate."},
			assert: func(t *testing.T, resp OpenAIResponse) {
				// Top-level fields — OpenAI contract.
				if resp.ID == "" {
					t.Fatal("ID must not be empty")
				}
				if !strings.HasPrefix(resp.ID, "chatcmpl-") {
					t.Errorf("ID = %q, want prefix %q", resp.ID, "chatcmpl-")
				}
				if resp.Object != "chat.completion" {
					t.Errorf("Object = %q, want %q", resp.Object, "chat.completion")
				}
				if resp.Created == 0 {
					t.Fatal("Created must not be zero")
				}
				if resp.Created > time.Now().Unix() {
					t.Errorf("Created = %d, must not be in the future", resp.Created)
				}
				if resp.Model != "test-model" {
					t.Errorf("Model = %q, want %q", resp.Model, "test-model")
				}

				// Choices array.
				if len(resp.Choices) != 1 {
					t.Fatalf("len(Choices) = %d, want 1", len(resp.Choices))
				}
				c := resp.Choices[0]
				if c.Index != 0 {
					t.Errorf("Choice[0].Index = %d, want 0", c.Index)
				}

				// Non-stream uses Message, not Delta.
				if c.Delta != nil {
					t.Fatal("non-stream Choice must not have Delta")
				}
				if c.Message == nil {
					t.Fatal("non-stream Choice.Message must not be nil")
				}
				if c.Message.Role != "assistant" {
					t.Errorf("Choice[0].Message.Role = %q, want %q", c.Message.Role, "assistant")
				}
				if c.Message.Content != "Hello from mock substrate." {
					t.Errorf("Choice[0].Message.Content = %q, want %q",
						c.Message.Content, "Hello from mock substrate.")
				}
				if c.FinishReason != "stop" {
					t.Errorf("Choice[0].FinishReason = %q, want %q", c.FinishReason, "stop")
				}
			},
		},
	}

	for _, tt := range table {
		t.Run(tt.name, func(t *testing.T) {
			mockSub := newMockSubstrate(t, mockSubstrateCfg{
				Message:      tt.callback.Message,
				PostCallback: true,
				Delay:        10 * time.Millisecond,
			})
			defer mockSub.Close()

			_, ts := newTestBridge(t, mockSub.URL)
			defer ts.Close()

			req := OpenAIRequest{
				Model:    "test-model",
				Stream:   false,
				Messages: []Message{{Role: "user", Content: "Hello"}},
			}

			resp, _ := doCompletion(t, ts.URL, req)
			tt.assert(t, resp)
		})
	}
}

func TestBridge_StreamingFlow(t *testing.T) {
	mockSub := newMockSubstrate(t, mockSubstrateCfg{
		Message:      "chunk1 chunk2 chunk3",
		PostCallback: true,
		Delay:        10 * time.Millisecond,
	})
	defer mockSub.Close()

	_, ts := newTestBridge(t, mockSub.URL)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json",
		bytes.NewReader([]byte(`{"model":"test-model","messages":[{"role":"user","content":"Hello"}],"stream":true}`)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}

	raw, _ := io.ReadAll(resp.Body)
	rawStr := string(raw)

	// Verify SSE format: chunks are prefixed with "data: " and terminated.
	if !strings.Contains(rawStr, "data: {") {
		t.Error("response missing SSE data: prefix")
	}
	if !strings.Contains(rawStr, "[DONE]") {
		t.Error("response missing [DONE] terminator")
	}

	// Parse the SSE data line and assert the embedded JSON structure.
	var chunk OpenAIResponse
	lines := strings.Split(rawStr, "\n")
	var foundChunk bool
	for _, line := range lines {
		if !strings.HasPrefix(line, "data: {") {
			continue
		}
		if strings.HasPrefix(line, "data: [DONE]") {
			break
		}
		if err := json.Unmarshal([]byte(line[6:]), &chunk); err != nil {
			t.Fatalf("failed to parse SSE chunk JSON: %v", err)
		}
		foundChunk = true
		break
	}
	if !foundChunk {
		t.Fatal("no SSE data chunks found")
	}

	// Streaming uses chunk object, not completion object.
	if chunk.Object != "chat.completion.chunk" {
		t.Errorf("chunk Object = %q, want %q", chunk.Object, "chat.completion.chunk")
	}
	if chunk.ID == "" {
		t.Fatal("chunk ID must not be empty")
	}
	if !strings.HasPrefix(chunk.ID, "chatcmpl-") {
		t.Errorf("chunk ID = %q, want prefix %q", chunk.ID, "chatcmpl-")
	}
	if chunk.Model != "test-model" {
		t.Errorf("chunk Model = %q, want %q", chunk.Model, "test-model")
	}
	if chunk.Created == 0 {
		t.Fatal("chunk Created must not be zero")
	}

	// Streaming uses Delta, not Message.
	if len(chunk.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(chunk.Choices))
	}
	c := chunk.Choices[0]
	if c.Index != 0 {
		t.Errorf("Choice[0].Index = %d, want 0", c.Index)
	}
	if c.Message != nil {
		t.Fatal("streaming Choice must not have Message")
	}
	if c.Delta == nil {
		t.Fatal("streaming Choice.Delta must not be nil")
	}
	if c.Delta.Content != "chunk1 chunk2 chunk3" {
		t.Errorf("Choice[0].Delta.Content = %q, want %q",
			c.Delta.Content, "chunk1 chunk2 chunk3")
	}
}

func TestBridge_Completion_WithReasoning(t *testing.T) {
	mockSub := newMockSubstrate(t, mockSubstrateCfg{
		Message:      "answer",
		Reasoning:    "because I said so",
		PostCallback: true,
	})
	defer mockSub.Close()

	_, ts := newTestBridge(t, mockSub.URL)
	defer ts.Close()

	resp, _ := doCompletion(t, ts.URL, OpenAIRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	})

	// Top-level contract fields.
	if resp.Object != "chat.completion" {
		t.Errorf("Object = %q, want %q", resp.Object, "chat.completion")
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(resp.Choices))
	}
	c := resp.Choices[0]
	if c.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", c.FinishReason, "stop")
	}
	if c.Message == nil {
		t.Fatal("Message must not be nil")
	}
	if c.Message.ReasoningContent != "because I said so" {
		t.Errorf("ReasoningContent = %q, want %q",
			c.Message.ReasoningContent, "because I said so")
	}
}

func TestBridge_Completion_NoReasoning(t *testing.T) {
	mockSub := newMockSubstrate(t, mockSubstrateCfg{
		Message:      "answer",
		Reasoning:    "",
		PostCallback: true,
	})
	defer mockSub.Close()

	_, ts := newTestBridge(t, mockSub.URL)
	defer ts.Close()

	resp, _ := doCompletion(t, ts.URL, OpenAIRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	})

	if len(resp.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Message.ReasoningContent != "" {
		t.Errorf("ReasoningContent = %q, want empty", resp.Choices[0].Message.ReasoningContent)
	}
}

func TestBridge_Completion_CallbackNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test: bridge has a hard-coded 5-minute select timeout")
	}
	// Substrate accepts the request but never sends a callback —
	// the bridge should return Gateway Timeout after 5 minutes.
	mockSub := newMockSubstrate(t, mockSubstrateCfg{
		PostCallback: false,
	})
	defer mockSub.Close()

	_, ts := newTestBridge(t, mockSub.URL)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json",
		bytes.NewReader([]byte(`{"model":"test","messages":[{"role":"user","content":"hi"}]}`)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusGatewayTimeout)
	}
}

func Test_formatSSEChunk(t *testing.T) {
	got := formatSSEChunk("cmpl-123", "gpt-4", "hello world", "reasoning here")

	// Verify SSE framing.
	if !strings.HasPrefix(got, "data: {") {
		t.Fatal("expected SSE data: prefix")
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Error("expected SSE trailing newline")
	}

	// Parse the embedded JSON and assert the full structure.
	jsonStr := strings.TrimPrefix(got, "data: ")
	var chunk OpenAIResponse
	if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil {
		t.Fatalf("invalid SSE JSON: %v", err)
	}

	if chunk.ID != "cmpl-123" {
		t.Errorf("ID = %q, want %q", chunk.ID, "cmpl-123")
	}
	if chunk.Object != "chat.completion.chunk" {
		t.Errorf("Object = %q, want %q", chunk.Object, "chat.completion.chunk")
	}
	if chunk.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", chunk.Model, "gpt-4")
	}
	if chunk.Created == 0 {
		t.Fatal("Created must not be zero")
	}
	if len(chunk.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(chunk.Choices))
	}
	c := chunk.Choices[0]
	if c.Index != 0 {
		t.Errorf("Choice[0].Index = %d, want 0", c.Index)
	}
	if c.Delta == nil {
		t.Fatal("Delta must not be nil")
	}
	if c.Delta.Content != "hello world" {
		t.Errorf("Delta.Content = %q, want %q", c.Delta.Content, "hello world")
	}
	if c.Delta.ReasoningContent != "reasoning here" {
		t.Errorf("Delta.ReasoningContent = %q, want %q",
			c.Delta.ReasoningContent, "reasoning here")
	}
	if c.Message != nil {
		t.Fatal("streaming chunk must not have Message")
	}
}

func Test_formatSSEChunk_NoReasoning(t *testing.T) {
	got := formatSSEChunk("cmpl-456", "gpt-4", "hi", "")

	var chunk OpenAIResponse
	jsonStr := strings.TrimPrefix(got, "data: ")
	if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil {
		t.Fatalf("invalid SSE JSON: %v", err)
	}

	if len(chunk.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(chunk.Choices))
	}
	if chunk.Choices[0].Delta.ReasoningContent != "" {
		t.Errorf("Delta.ReasoningContent = %q, want empty",
			chunk.Choices[0].Delta.ReasoningContent)
	}
}

func TestBridge_HandlesEmptyMessages(t *testing.T) {
	mockSub := newMockSubstrate(t, mockSubstrateCfg{
		Message:      "ok",
		PostCallback: true,
	})
	defer mockSub.Close()

	_, ts := newTestBridge(t, mockSub.URL)
	defer ts.Close()

	// Request with an empty-string message — should still work.
	resp, _ := doCompletion(t, ts.URL, OpenAIRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: ""}},
	})

	// Full structure — empty content in request must not break the response.
	if resp.Object != "chat.completion" {
		t.Errorf("Object = %q, want %q", resp.Object, "chat.completion")
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(resp.Choices))
	}
	c := resp.Choices[0]
	if c.Message == nil {
		t.Fatal("Message must not be nil")
	}
	if c.Message.Content != "ok" {
		t.Errorf("Content = %q, want %q", c.Message.Content, "ok")
	}
	if c.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", c.FinishReason, "stop")
	}
}

func TestBridge_HandleCallback_MalformedRequest(t *testing.T) {
	_, ts := newTestBridge(t, "http://unused")
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/callback", "application/json",
		bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestBridge_HandleCallback_MethodNotAllowed(t *testing.T) {
	_, ts := newTestBridge(t, "http://unused")
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/callback")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestBridge_HandleCompletion_MethodNotAllowed(t *testing.T) {
	_, ts := newTestBridge(t, "http://unused")
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/chat/completions")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}
