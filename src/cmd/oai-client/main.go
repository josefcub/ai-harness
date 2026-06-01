package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	defaultPort         = 8088
	defaultChannel      = "openwebui"
	defaultSubstrateURL = "http://127.0.0.1:8080/webhook"
)

// ---------- OpenAI Types ----------

type OpenAIRequest struct {
	Model            string      `json:"model"`
	Messages         []Message   `json:"messages"`
	Stream           bool        `json:"stream"`
	Tools            interface{} `json:"tools,omitempty"`
	SessionID        string      `json:"session_id,omitempty"`
	SubstrateChannel string      `json:"substrate_channel,omitempty"`
}

type Message struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
}

type OpenAIResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Index        int      `json:"index"`
	Delta        *Delta   `json:"delta,omitempty"`
	Message      *Message `json:"message,omitempty"`
	FinishReason string   `json:"finish_reason"`
}

type Delta struct {
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// ---------- Substrate Types ----------

type SubstrateRequest struct {
	Channel         string                 `json:"channel"`
	Message         string                 `json:"message"`
	CallbackURL     string                 `json:"callback_url,omitempty"`
	ImageAttachment *ImageAttachment       `json:"image_attachment,omitempty"`
}

type ImageAttachment struct {
	Data     string `json:"data"`
	MIMEType string `json:"mime_type"`
}

type SubstrateCallback struct {
	Channel string `json:"channel"`
	Message string `json:"message"`
	Reasoning string `json:"reasoning,omitempty"`
}

// ---------- Bridge ----------

type Bridge struct {
	substrateURL    string
	callbackBaseURL string
	substrateClient *http.Client
	callbacks       map[string]chan SubstrateCallback
	mu              sync.Mutex
}

func NewBridge(substrateURL string) *Bridge {
	return &Bridge{
		substrateURL:    substrateURL,
		substrateClient: &http.Client{Timeout: 5 * time.Minute},
		callbacks:       make(map[string]chan SubstrateCallback),
	}
}

func (b *Bridge) SetCallbackBaseURL(url string) {
	b.callbackBaseURL = url
}

func formatSSEChunk(id, model, content string, reasoning string) string {
	chunk := OpenAIResponse{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{
			{
				Index: 0,
				Delta: &Delta{Content: content, ReasoningContent: reasoning},
			},
		},
	}
	data, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", data)
}

func (b *Bridge) HandleCompletion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read and log full request body for session heuristic analysis
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("[ERROR] Failed to read request body: %v", err)
		http.Error(w, "failed to read body", http.StatusInternalServerError)
		return
	}
	log.Printf("[DEBUG] Full Request Body: %s", string(bodyBytes))
	log.Printf("[DEBUG] Request Headers: %v", r.Header)
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req OpenAIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[ERROR] Invalid request JSON: %v", err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	log.Printf("[DEBUG] Received OpenAI request for model: %s, stream: %v, messages: %d, substrate_channel: %s", req.Model, req.Stream, len(req.Messages), req.SubstrateChannel)

	// Convert messages to string
	var msgContent string
	for _, m := range req.Messages {
		if s, ok := m.Content.(string); ok {
			msgContent = s
		}
	}

	// Determine session ID
	sessionID := req.SubstrateChannel
	if sessionID == "" {
		sessionID = fmt.Sprintf("openai-endpoint-channel-%s", time.Now().Format("20060102"))
	}

	// Generate unique request ID for internal tracking
	reqID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())

	// Register callback channel for both reqID and sessionID
	b.mu.Lock()
	cbCh := make(chan SubstrateCallback, 1)
	b.callbacks[reqID] = cbCh
	b.callbacks[sessionID] = cbCh
	b.mu.Unlock()

	// Use sessionID as channel to map callback back
	callbackURL := "http://127.0.0.1:8088/callback"
	if b.callbackBaseURL != "" {
		callbackURL = b.callbackBaseURL + "/callback"
	}
	substrateReq := SubstrateRequest{
		Channel:     sessionID,
		Message:     msgContent,
		CallbackURL: callbackURL,
	}

	payload, err := json.Marshal(substrateReq)
	if err != nil {
		log.Printf("[ERROR] Marshal error for reqID %s: %v", reqID, err)
		http.Error(w, "failed to prepare substrate request", http.StatusInternalServerError)
		return
	}

	resp, err := b.substrateClient.Post(b.substrateURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("[ERROR] Substrate connection failed for reqID %s: %v", reqID, err)
		http.Error(w, "substrate connection failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Wait for callback
	select {
	case cb := <-cbCh:
		log.Printf("[DEBUG] Callback received for reqID %s, length: %d", reqID, len(cb.Message))
		if req.Stream {
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "streaming not supported", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			flusher.Flush()

			w.Write([]byte(formatSSEChunk(reqID, req.Model, cb.Message, cb.Reasoning)))
			flusher.Flush()
			w.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
		} else {
			openAIResp := OpenAIResponse{
				ID:      reqID,
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []Choice{
					{
						Index:        0,
						Message:      &Message{Role: "assistant", Content: cb.Message},
						FinishReason: "stop",
					},
				},
			}
			if cb.Reasoning != "" {
				openAIResp.Choices[0].Message.ReasoningContent = cb.Reasoning
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(openAIResp)
		}
	case <-time.After(5 * time.Minute):
		log.Printf("[ERROR] Timeout waiting for substrate callback for reqID %s", reqID)
		http.Error(w, "gateway timeout: substrate did not respond within 5 minutes", http.StatusGatewayTimeout)
	}

	// Cleanup
	b.mu.Lock()
	delete(b.callbacks, reqID)
	delete(b.callbacks, sessionID)
	b.mu.Unlock()
}

func (b *Bridge) HandleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	models := struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}{
		Object: "list",
		Data: []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		}{
			{
				ID:      "default",
				Object:  "model",
				Created: time.Now().Unix(),
				OwnedBy: "substrate",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models)
}

func (b *Bridge) HandleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cb SubstrateCallback
	if err := json.NewDecoder(r.Body).Decode(&cb); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Find the request ID from the channel?
	// The substrate callback only sends channel and message.
	// We need a way to map this back to the OpenAI request.
	// Since we can't easily get the request ID from the callback,
	// we might need to rely on the channel name or a different mapping.
	//
	// *Correction*: The substrate webhook processes messages one by one.
	// If we use a unique channel per request, we can map it.
	// But that's heavy.
	//
	// *Better approach*: The `SubstrateRequest` has a `Channel`.
	// We can use the `reqID` as the `Channel` for this specific request.
	// Then the callback will tell us which channel (reqID) it came from.

	log.Printf("[DEBUG] Callback received for channel: %s, message length: %d", cb.Channel, len(cb.Message))

	if ch, ok := b.callbacks[cb.Channel]; ok {
		select {
		case ch <- cb:
		default:
			// Channel full or already read
			log.Printf("[WARN] Callback channel full or consumed for %s", cb.Channel)
		}
	} else {
		log.Printf("[ERROR] Unexpected callback for unknown channel: %s", cb.Channel)
	}

	w.WriteHeader(http.StatusOK)
}

func main() {
	port := defaultPort
	substrateURL := defaultSubstrateURL

	if p := os.Getenv("PORT"); p != "" {
		if parsedPort, err := strconv.Atoi(p); err == nil {
			port = parsedPort
		} else {
			log.Printf("Invalid PORT value '%s', using default %d", p, defaultPort)
		}
	}

	if su := os.Getenv("SUBSTRATE_URL"); su != "" {
		substrateURL = su
	}

	// Initialize persistent debug log for session heuristic analysis
	debugLog, err := os.OpenFile("memory/substrate_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		log.SetOutput(debugLog)
		log.Printf("[DEBUG] Substrate bridge starting on port %d", port)
	} else {
		log.Printf("[WARN] Failed to open debug log: %v", err)
	}

	bridge := NewBridge(substrateURL)

	http.HandleFunc("/v1/chat/completions", bridge.HandleCompletion)
	http.HandleFunc("/callback", bridge.HandleCallback)
	http.HandleFunc("/v1/models", bridge.HandleModels)

	addr := fmt.Sprintf(":%d", port)
	log.Printf("OpenAI Endpoint Client listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
