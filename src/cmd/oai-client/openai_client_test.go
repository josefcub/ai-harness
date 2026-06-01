package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBridge_MalformedPayload(t *testing.T) {
	// 1. Mock Substrate
	mockSubstrate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockSubstrate.Close()

	// 2. Create Bridge
	bridge := NewBridge(mockSubstrate.URL)

	// 3. Test Server
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", bridge.HandleCompletion)
	mux.HandleFunc("/callback", bridge.HandleCallback)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	bridge.SetCallbackBaseURL(ts.URL)

	// 4. Send Malformed Request
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// 5. Verify Response (Expect 400 Bad Request or similar)
	if resp.StatusCode == http.StatusOK {
		t.Error("expected error status for malformed payload, got OK")
	}
}

func TestBridge_FullFlow(t *testing.T) {
	var wg sync.WaitGroup
	var wgDone sync.WaitGroup
	wg.Add(1)
	wgDone.Add(1)

	// 1. Mock Substrate
	mockSubstrate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SubstrateRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify we used reqID as channel
		if req.Channel == "" {
			t.Error("expected channel to be set")
		}

		// Simulate Substrate processing delay
		time.Sleep(100 * time.Millisecond)

		// Send actual callback request
		cb := SubstrateCallback{
			Channel: req.Channel,
			Message: "Hello from mock substrate.",
		}
		cbBody, _ := json.Marshal(cb)
		go func() {
			http.Post(req.CallbackURL, "application/json", bytes.NewReader(cbBody))
		}()

		// Respond to the webhook request
		w.WriteHeader(http.StatusOK)
		wg.Done()
	}))
	defer mockSubstrate.Close()

	// 2. Create Bridge
	bridge := NewBridge(mockSubstrate.URL)

	// 3. Test Server (handles both completion and callback)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", bridge.HandleCompletion)
	mux.HandleFunc("/callback", bridge.HandleCallback)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	bridge.SetCallbackBaseURL(ts.URL)

	// 4. Send Request
	openAIReq := OpenAIRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	}
	body, _ := json.Marshal(openAIReq)

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	// 5. Verify Response
	var openAIResp OpenAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if len(openAIResp.Choices) == 0 {
		t.Fatal("no choices in response")
	}

	expectedMsg := "Hello from mock substrate."
	if openAIResp.Choices[0].Message.Content != expectedMsg {
		t.Errorf("expected message %q, got %q", expectedMsg, openAIResp.Choices[0].Message.Content)
	}

	wg.Wait()
}

func TestBridge_StreamingFlow(t *testing.T) {
	var wg sync.WaitGroup
	var wgDone sync.WaitGroup
	wg.Add(1)
	wgDone.Add(1)

	mockSubstrate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SubstrateRequest
		json.NewDecoder(r.Body).Decode(&req)

		time.Sleep(100 * time.Millisecond)

		cb := SubstrateCallback{
			Channel: req.Channel,
			Message: "chunk1 chunk2 chunk3",
		}
		cbBody, _ := json.Marshal(cb)
		go func() {
			http.Post(req.CallbackURL, "application/json", bytes.NewReader(cbBody))
		}()

		w.WriteHeader(http.StatusOK)
		wg.Done()
	}))
	defer mockSubstrate.Close()

	bridge := NewBridge(mockSubstrate.URL)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", bridge.HandleCompletion)
	mux.HandleFunc("/callback", bridge.HandleCallback)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	bridge.SetCallbackBaseURL(ts.URL)

	openAIReq := OpenAIRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
		Stream: true,
	}
	body, _ := json.Marshal(openAIReq)

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)

	if !strings.Contains(bodyStr, "data:") {
		t.Error("expected SSE data format")
	}
	if !strings.Contains(bodyStr, "[DONE]") {
		t.Error("expected [DONE] message")
	}
	if !strings.Contains(bodyStr, "chunk1 chunk2 chunk3") {
		t.Error("expected message content in stream")
	}

	wg.Wait()
}

func TestBridge_Timeout(t *testing.T) {
	mockSubstrate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SubstrateRequest
		json.NewDecoder(r.Body).Decode(&req)
		// Simulate a very slow substrate that never calls back
		time.Sleep(10 * time.Minute)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockSubstrate.Close()

	bridge := NewBridge(mockSubstrate.URL)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", bridge.HandleCompletion)
	mux.HandleFunc("/callback", bridge.HandleCallback)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	bridge.SetCallbackBaseURL(ts.URL)

	openAIReq := OpenAIRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	}
	body, _ := json.Marshal(openAIReq)

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("expected 504 Gateway Timeout, got %d", resp.StatusCode)
	}
}
