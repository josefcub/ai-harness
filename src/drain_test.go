package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/agent-project/harness/log"
	"github.com/agent-project/harness/queue"
	"github.com/agent-project/harness/session"
)

// TestDrainPending_EmptyQueue verifies that draining an empty queue
// returns 0 and does not create any session files.
func TestDrainPending_EmptyQueue(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))
	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	count := drainPending(q, sessions, logger)

	if count != 0 {
		t.Errorf("expected 0 drained messages, got %d", count)
	}

	if sessions.Count() != 0 {
		t.Errorf("expected 0 sessions after draining empty queue, got %d", sessions.Count())
	}
}

// TestDrainPending_SingleMessage verifies that a single pending message
// is drained to the correct session with correct content.
func TestDrainPending_SingleMessage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))
	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	q.Enqueue(queue.Message{
		ChannelID:   "slack:abc123",
		MessageText: "hello world",
	})

	count := drainPending(q, sessions, logger)

	if count != 1 {
		t.Fatalf("expected 1 drained message, got %d", count)
	}

	s := sessions.Get("slack:abc123")
	if s == nil {
		t.Fatal("expected session for slack:abc123 to exist")
	}

	if len(s.Messages) != 1 {
		t.Fatalf("expected 1 message in session, got %d", len(s.Messages))
	}

	msg := s.Messages[0]
	if msg.Role != session.RoleUser {
		t.Errorf("expected role %q, got %q", session.RoleUser, msg.Role)
	}
	if msg.Content != "hello world" {
		t.Errorf("expected content %q, got %q", "hello world", msg.Content)
	}
	if msg.ToolCallID != "" {
		t.Errorf("expected empty ToolCallID, got %q", msg.ToolCallID)
	}
}

// TestDrainPending_MultipleChannels verifies that messages from different
// channels are drained to separate sessions.
func TestDrainPending_MultipleChannels(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))
	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	q.Enqueue(queue.Message{
		ChannelID:   "slack:aaa",
		MessageText: "message from aaa",
	})
	q.Enqueue(queue.Message{
		ChannelID:   "slack:bbb",
		MessageText: "message from bbb",
	})
	q.Enqueue(queue.Message{
		ChannelID:   "slack:aaa",
		MessageText: "second from aaa",
	})

	count := drainPending(q, sessions, logger)

	if count != 3 {
		t.Fatalf("expected 3 drained messages, got %d", count)
	}

	// Verify slack:aaa has 2 messages
	sAaa := sessions.Get("slack:aaa")
	if sAaa == nil {
		t.Fatal("expected session for slack:aaa")
	}
	if len(sAaa.Messages) != 2 {
		t.Fatalf("expected 2 messages for slack:aaa, got %d", len(sAaa.Messages))
	}
	if sAaa.Messages[0].Content != "message from aaa" {
		t.Errorf("slack:aaa[0]: expected %q, got %q", "message from aaa", sAaa.Messages[0].Content)
	}
	if sAaa.Messages[1].Content != "second from aaa" {
		t.Errorf("slack:aaa[1]: expected %q, got %q", "second from aaa", sAaa.Messages[1].Content)
	}

	// Verify slack:bbb has 1 message
	sBbb := sessions.Get("slack:bbb")
	if sBbb == nil {
		t.Fatal("expected session for slack:bbb")
	}
	if len(sBbb.Messages) != 1 {
		t.Fatalf("expected 1 message for slack:bbb, got %d", len(sBbb.Messages))
	}
	if sBbb.Messages[0].Content != "message from bbb" {
		t.Errorf("slack:bbb[0]: expected %q, got %q", "message from bbb", sBbb.Messages[0].Content)
	}
}

// TestDrainPending_ImageAttachment verifies that image attachments are
// preserved when draining messages.
func TestDrainPending_ImageAttachment(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))
	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	q.Enqueue(queue.Message{
		ChannelID:   "slack:img",
		MessageText: "look at this",
		ImageAttachment: session.ImageAttachment{
			Data:     "base64data",
			MIMEType: "image/png",
		},
	})

	count := drainPending(q, sessions, logger)

	if count != 1 {
		t.Fatalf("expected 1 drained message, got %d", count)
	}

	s := sessions.Get("slack:img")
	if s == nil {
		t.Fatal("expected session for slack:img")
	}
	if len(s.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(s.Messages))
	}

	msg := s.Messages[0]
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}
	if msg.Attachments[0].Data != "base64data" {
		t.Errorf("expected attachment data %q, got %q", "base64data", msg.Attachments[0].Data)
	}
	if msg.Attachments[0].MIMEType != "image/png" {
		t.Errorf("expected MIME type %q, got %q", "image/png", msg.Attachments[0].MIMEType)
	}
}

// TestDrainPending_AppendsToExistingSession verifies that draining appends
// to an existing session rather than replacing it.
func TestDrainPending_AppendsToExistingSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))
	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	// Pre-existing session with a message
	existing := sessions.Get("slack:existing")
	existing.Messages = append(existing.Messages, session.ConversationMessage{
		Role:    session.RoleUser,
		Content: "existing message",
	})

	// Enqueue a new message for the same channel
	q.Enqueue(queue.Message{
		ChannelID:   "slack:existing",
		MessageText: "drained message",
	})

	count := drainPending(q, sessions, logger)

	if count != 1 {
		t.Fatalf("expected 1 drained message, got %d", count)
	}

	s := sessions.Get("slack:existing")
	if len(s.Messages) != 2 {
		t.Fatalf("expected 2 messages in session, got %d", len(s.Messages))
	}
	if s.Messages[0].Content != "existing message" {
		t.Errorf("expected first message %q, got %q", "existing message", s.Messages[0].Content)
	}
	if s.Messages[1].Content != "drained message" {
		t.Errorf("expected second message %q, got %q", "drained message", s.Messages[1].Content)
	}
}

// TestDrainPending_PersistsToDisk verifies that drained sessions are written
// to disk and can be read back.
func TestDrainPending_PersistsToDisk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))
	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	q.Enqueue(queue.Message{
		ChannelID:   "slack:disk",
		MessageText: "persist me",
	})

	drainPending(q, sessions, logger)

	// SaveAll should persist the drained session
	if err := sessions.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	// Read the file back (SanitizeFilename replaces ':' with '_')
	data, err := os.ReadFile(filepath.Join(dir, "state", "slack_disk.json"))
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}

	var s session.Session
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse session: %v", err)
	}

	if len(s.Messages) != 1 {
		t.Fatalf("expected 1 message in persisted session, got %d", len(s.Messages))
	}
	if s.Messages[0].Content != "persist me" {
		t.Errorf("expected content %q, got %q", "persist me", s.Messages[0].Content)
	}
	if s.ChannelID != "slack:disk" {
		t.Errorf("expected channel %q, got %q", "slack:disk", s.ChannelID)
	}
	if s.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if s.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
}

// TestDrainPending_NilLogger verifies that a nil logger does not cause a panic.
func TestDrainPending_NilLogger(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))

	q.Enqueue(queue.Message{
		ChannelID:   "slack:noop",
		MessageText: "silent drain",
	})

	// Should not panic with nil logger
	count := drainPending(q, sessions, nil)

	if count != 1 {
		t.Fatalf("expected 1 drained message, got %d", count)
	}
}

// TestDrainPending_MultipleMessagesSameChannelPreservesOrder verifies that
// messages for the same channel are appended in FIFO order.
func TestDrainPending_MultipleMessagesSameChannelPreservesOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))
	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	// Enqueue 5 messages for the same channel
	for i := 0; i < 5; i++ {
		q.Enqueue(queue.Message{
			ChannelID:   "slack:ordered",
			MessageText: msgText(i),
		})
	}

	count := drainPending(q, sessions, logger)

	if count != 5 {
		t.Fatalf("expected 5 drained messages, got %d", count)
	}

	s := sessions.Get("slack:ordered")
	if len(s.Messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(s.Messages))
	}

	for i := 0; i < 5; i++ {
		if s.Messages[i].Content != msgText(i) {
			t.Errorf("message[%d]: expected %q, got %q", i, msgText(i), s.Messages[i].Content)
		}
	}
}

// TestDrainPending_ReturnValueMatchesDrained verifies the return value
// equals the number of messages that were successfully persisted.
func TestDrainPending_ReturnValueMatchesDrained(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))
	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	// Enqueue 3 messages
	for i := 0; i < 3; i++ {
		q.Enqueue(queue.Message{
			ChannelID:   "slack:ret",
			MessageText: msgText(i),
		})
	}

	count := drainPending(q, sessions, logger)

	if count != 3 {
		t.Errorf("expected return value 3, got %d", count)
	}

	// Verify session count matches
	s := sessions.Get("slack:ret")
	if len(s.Messages) != 3 {
		t.Errorf("expected 3 messages in session, got %d", len(s.Messages))
	}
}

// TestDrainPending_Attributes verifies that drained messages have correct
// metadata: no ToolCalls, no ToolCallID, no ReasoningContent.
func TestDrainPending_Attributes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))
	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	q.Enqueue(queue.Message{
		ChannelID:   "slack:attr",
		MessageText: "attrs",
	})

	drainPending(q, sessions, logger)

	s := sessions.Get("slack:attr")
	msg := s.Messages[0]

	if msg.Role != session.RoleUser {
		t.Errorf("expected role %q, got %q", session.RoleUser, msg.Role)
	}
	if msg.ToolCallID != "" {
		t.Errorf("expected empty ToolCallID, got %q", msg.ToolCallID)
	}
	if len(msg.ToolCalls) != 0 {
		t.Errorf("expected no ToolCalls, got %d", len(msg.ToolCalls))
	}
	if msg.ReasoningContent != "" {
		t.Errorf("expected empty ReasoningContent, got %q", msg.ReasoningContent)
	}
	if msg.Summary {
		t.Error("expected Summary to be false")
	}
}

// TestDrainPending_CreatesNewSession verifies that draining a message for
// a channel with no existing session creates a new session with correct timestamps.
func TestDrainPending_CreatesNewSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	q := queue.New(10, nil)
	sessions := session.NewManager(filepath.Join(dir, "state"))
	logger, err := log.New(filepath.Join(dir, "logs"), log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	q.Enqueue(queue.Message{
		ChannelID:   "slack:new",
		MessageText: "new channel",
	})

	before := time.Now()
	count := drainPending(q, sessions, logger)
	after := time.Now()

	if count != 1 {
		t.Fatalf("expected 1 drained message, got %d", count)
	}

	s := sessions.Get("slack:new")
	if s.CreatedAt.Before(before) || s.CreatedAt.After(after) {
		t.Errorf("CreatedAt %v not between %v and %v", s.CreatedAt, before, after)
	}
	if s.UpdatedAt.Before(before) || s.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt %v not between %v and %v", s.UpdatedAt, before, after)
	}
}

func msgText(i int) string {
	return "message " + strconv.Itoa(i)
}
