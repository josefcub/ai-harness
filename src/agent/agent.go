package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/agent-project/harness/channellog"
	"github.com/agent-project/harness/llm"
	"github.com/agent-project/harness/log"
	"github.com/agent-project/harness/session"
	"github.com/agent-project/harness/tools"
)

// attachmentTokenCost is the estimated token cost per image attachment.
const attachmentTokenCost = 1000

// ChatClient is the interface for LLM chat completions.
type ChatClient interface {
	Chat(ctx context.Context, messages []llm.Message, toolsJSON json.RawMessage, maxTokens int) (*llm.ChatResponse, error)
}

// Agent processes messages through the LLM with tool-call support.
type Agent struct {
	client              ChatClient
	tools               *tools.Registry
	maxToolIterations   int
	contextTokens       int
	summarizeThreshold  float64
	summarizeKeepRecent int
	maxTokens           int
	logToolCalls        bool
	logAgentReasoning   bool
	channelLogger       *channellog.Logger
	logger              *log.Logger
}

// AgentOption configures an Agent.
type AgentOption func(*agentConfig)

type agentConfig struct {
	channelLogger       *channellog.Logger
	logger              *log.Logger
	maxToolIterations   int
	contextTokens       int
	summarizeThreshold  float64
	summarizeKeepRecent int
	maxTokens           int
	logToolCalls        bool
	logAgentReasoning   bool
}

// WithChannelLogger sets the channel conversation logger.
func WithChannelLogger(cl *channellog.Logger) AgentOption {
	return func(c *agentConfig) { c.channelLogger = cl }
}

// WithLogger sets the agent logger.
func WithLogger(l *log.Logger) AgentOption {
	return func(c *agentConfig) { c.logger = l }
}

// WithMaxToolIterations sets the maximum number of tool-call iterations.
func WithMaxToolIterations(n int) AgentOption {
	return func(c *agentConfig) { c.maxToolIterations = n }
}

// WithContextTokens sets the context window size in tokens.
func WithContextTokens(n int) AgentOption {
	return func(c *agentConfig) { c.contextTokens = n }
}

// WithSummarizeThreshold sets the threshold fraction for summarization.
func WithSummarizeThreshold(f float64) AgentOption {
	return func(c *agentConfig) { c.summarizeThreshold = f }
}

// WithSummarizeKeepRecent sets the number of recent messages to preserve during summarization.
func WithSummarizeKeepRecent(n int) AgentOption {
	return func(c *agentConfig) { c.summarizeKeepRecent = n }
}

// WithMaxTokens sets the maximum tokens for LLM responses.
func WithMaxTokens(n int) AgentOption {
	return func(c *agentConfig) { c.maxTokens = n }
}

// WithLogToolCalls enables or disables tool call logging.
func WithLogToolCalls(v bool) AgentOption {
	return func(c *agentConfig) { c.logToolCalls = v }
}

// WithLogAgentReasoning enables or disables agent reasoning logging.
func WithLogAgentReasoning(v bool) AgentOption {
	return func(c *agentConfig) { c.logAgentReasoning = v }
}

// New creates a new Agent.
// logger and channelLogger may be nil (logging calls are no-ops).
func New(client ChatClient, reg *tools.Registry, opts ...AgentOption) *Agent {
	cfg := agentConfig{
		maxToolIterations:   20,
		contextTokens:       8192,
		summarizeThreshold:  0.70,
		summarizeKeepRecent: 10,
		maxTokens:           4096,
		logToolCalls:        true,
		logAgentReasoning:   true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Agent{
		client:              client,
		tools:               reg,
		maxToolIterations:   cfg.maxToolIterations,
		contextTokens:       cfg.contextTokens,
		summarizeThreshold:  cfg.summarizeThreshold,
		summarizeKeepRecent: cfg.summarizeKeepRecent,
		maxTokens:           cfg.maxTokens,
		logToolCalls:        cfg.logToolCalls,
		logAgentReasoning:   cfg.logAgentReasoning,
		channelLogger:       cfg.channelLogger,
		logger:              cfg.logger,
	}
}

// buildUserMessage creates a user conversation message from the input text
// and optional image attachment.
func buildUserMessage(messageText string, imageAtt session.ImageAttachment) session.ConversationMessage {
	msg := session.ConversationMessage{
		Role:    session.RoleUser,
		Content: messageText,
	}
	if imageAtt.Data != "" {
		msg.Attachments = []session.ImageAttachment{imageAtt}
	}
	return msg
}

// recordAssistantMessage appends the LLM response to the session and returns
// the tool calls (if any). This eliminates duplicated session-append logic
// across the normal and error paths.
func recordAssistantMessage(sess *session.Session, resp *llm.ChatResponse) []llm.ToolCall {
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:             session.RoleAssistant,
		Content:          resp.Content,
		ReasoningContent: resp.ReasoningContent,
		ToolCalls:        convertLLMToolCalls(resp.ToolCalls),
	})
	return resp.ToolCalls
}

// dispatchLLM calls the LLM chat API with the given messages and tool
// definitions. On error with a partial response, it records the partial
// response in the session and accumulates output before returning.
func (a *Agent) dispatchLLM(ctx context.Context, sess *session.Session, messages []llm.Message, toolsJSON json.RawMessage, output *strings.Builder, iteration int) (*llm.ChatResponse, error) {
	defs := a.tools.Definitions()
	if len(defs) > 0 {
		var err error
		toolsJSON, err = json.Marshal(defs)
		if err != nil {
			return nil, fmt.Errorf("marshal tool definitions: %w", err)
		}
	}

	resp, err := a.client.Chat(ctx, messages, toolsJSON, a.maxTokens)
	if err != nil {
		if a.logger != nil {
			a.logger.Error("LLM call failed", "error", err.Error())
		}

		// Accumulate partial response content into session and output buffer
		if resp != nil {
			recordAssistantMessage(sess, resp)
			a.accumulateOutput(resp, output, false, a.logger)
			return resp, fmt.Errorf("LLM call interrupted (iteration %d, partial response saved): %w", iteration, err)
		}

		return resp, fmt.Errorf("LLM call failed (iteration %d): %w", iteration, err)
	}

	return resp, nil
}

// executeToolCalls logs and executes the given tool calls, accumulating
// output and appending results to the session.
func (a *Agent) executeToolCalls(sess *session.Session, toolCalls []llm.ToolCall, output *strings.Builder, logger *log.Logger) {
	for _, tc := range toolCalls {
		if a.logToolCalls && logger != nil {
			logger.Debug("tool call", "tool", tc.Function.Name, "id", tc.ID,
				"arguments", tc.Function.Arguments)
		}

		output.WriteString(fmt.Sprintf("\n[Tool Call: %s]\n", tc.Function.Name))

		result, err := a.tools.Dispatch(tc.Function.Name, tc.Function.Arguments)
		if err != nil {
			if a.logToolCalls && logger != nil {
				logger.Warn("tool error", "tool", tc.Function.Name, "error", err.Error())
			}
			result = err.Error()
		}

		if a.logToolCalls && logger != nil {
			logger.Debug("tool result", "tool", tc.Function.Name, "result", result)
		}

		// Log tool call to channel log
		if a.channelLogger != nil {
			_ = a.channelLogger.LogTool(sess.ChannelID, tc.Function.Name)
		}

		output.WriteString(fmt.Sprintf("[Result: %s]\n", result))

		// Parse result for embedded attachments (e.g. images from view_image)
		text, attachments := parseToolResult(result)

		// Append tool result to session
		toolMsg := session.ConversationMessage{
			Role:       session.RoleTool,
			Content:    text,
			ToolCallID: tc.ID,
		}
		if len(attachments) > 0 {
			toolMsg.Attachments = attachments
		}
		sess.Messages = append(sess.Messages, toolMsg)
	}
}

// Process runs the tool-call loop for a single user message and returns the
// aggregated output string. The user message is appended to the session before
// processing begins.
func (a *Agent) Process(ctx context.Context, sess *session.Session, messageText, systemPrompt string, imageAtt session.ImageAttachment) (string, error) {
	// Build and record user message
	msg := buildUserMessage(messageText, imageAtt)
	sess.Messages = append(sess.Messages, msg)

	// Log user message to channel log
	if a.channelLogger != nil {
		_ = a.channelLogger.LogUser(sess.ChannelID, messageText)
	}

	logger := a.logger
	var output strings.Builder

	for i := 0; i < a.maxToolIterations; i++ {
		// Summarize context if needed to stay within context window
		if err := a.summarizeIfNeeded(ctx, sess, systemPrompt); err != nil {
			return output.String(), err
		}

		// Build messages for LLM request
		messages := a.toLLMMessages(sess, systemPrompt)

		// Dispatch to LLM
		resp, err := a.dispatchLLM(ctx, sess, messages, nil, &output, i)
		if err != nil {
			return output.String(), err
		}

		// Log and accumulate agent response
		a.accumulateOutput(resp, &output, a.logAgentReasoning, logger)

		// If no tool calls, record the final assistant message and we're done
		if len(resp.ToolCalls) == 0 {
			recordAssistantMessage(sess, resp)
			// Log final assistant message to channel log
			if resp.Content != "" && a.channelLogger != nil {
				_ = a.channelLogger.LogAssistant(sess.ChannelID, resp.Content)
			}
			break
		}

		// Record assistant message with tool calls on session
		recordAssistantMessage(sess, resp)

		// Execute tool calls
		a.executeToolCalls(sess, resp.ToolCalls, &output, logger)
	}

	// If max iterations exhausted and last message is a tool result or
	// assistant message with tool calls (no final text), append a synthetic
	// closing message so session state is valid.
	lastMsg := sess.LastMessage()
	if lastMsg != nil && (lastMsg.Role == session.RoleTool || len(lastMsg.ToolCalls) > 0) {
		if logger != nil {
			logger.Warn("max tool iterations reached — appended synthetic closing message")
		}
		sess.Messages = append(sess.Messages, session.ConversationMessage{
			Role:    session.RoleAssistant,
			Content: "I reached my tool call limit this turn. Would you like me to continue?",
		})
		output.WriteString("\nI reached my tool call limit this turn. Would you like me to continue?")
	}

	return output.String(), nil
}

// accumulateOutput writes reasoning content and text content from a response
// into the output buffer. When logReasoning is true and logger is non-nil, it
// also emits debug logs for both fields.
func (a *Agent) accumulateOutput(resp *llm.ChatResponse, output *strings.Builder, logReasoning bool, logger *log.Logger) {
	if resp.ReasoningContent != "" {
		if logReasoning && logger != nil {
			logger.Debug("agent reasoning", "content", resp.ReasoningContent)
		}
		output.WriteString("[Reasoning: " + resp.ReasoningContent + "]\n")
	}
	if resp.Content != "" {
		if logReasoning && logger != nil {
			logger.Debug("agent response", "content", resp.Content)
		}
		output.WriteString(resp.Content)
	}
}

// convertLLMToolCalls converts a slice of LLM tool calls to session tool calls.
func convertLLMToolCalls(tcs []llm.ToolCall) []session.ToolCall {
	if len(tcs) == 0 {
		return nil
	}
	sessTCs := make([]session.ToolCall, 0, len(tcs))
	for _, tc := range tcs {
		sessTCs = append(sessTCs, session.ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
		})
		sessTCs[len(sessTCs)-1].Function.Name = tc.Function.Name
		sessTCs[len(sessTCs)-1].Function.Arguments = tc.Function.Arguments
	}
	return sessTCs
}

// convertSessionToolCalls converts a slice of session tool calls to LLM tool calls.
func convertSessionToolCalls(tcs []session.ToolCall) []llm.ToolCall {
	if len(tcs) == 0 {
		return nil
	}
	llmTCs := make([]llm.ToolCall, 0, len(tcs))
	for _, tc := range tcs {
		llmTCs = append(llmTCs, llm.ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
		})
		llmTCs[len(llmTCs)-1].Function.Name = tc.Function.Name
		llmTCs[len(llmTCs)-1].Function.Arguments = tc.Function.Arguments
	}
	return llmTCs
}

// summarizeIfNeeded checks whether context is approaching the limit and triggers
// summarization if so. Returns nil if no summarization was needed or it succeeded,
// or an error if summarization failed (caller should stop processing).
func (a *Agent) summarizeIfNeeded(ctx context.Context, sess *session.Session, systemPrompt string) error {
	if a.contextTokens <= 0 {
		return nil
	}
	if sess == nil {
		return nil
	}

	totalTokens := a.totalTokens(sess, systemPrompt)
	limit := int(float64(a.contextTokens) * a.summarizeThreshold)

	if totalTokens <= limit {
		return nil
	}

	return a.summarizeContext(ctx, sess)
}

// summarizeContext compresses older messages in the session into a summary
// to stay within the context window. The most recent messages are preserved.
func (a *Agent) summarizeContext(ctx context.Context, sess *session.Session) error {
	logger := a.logger

	// Log start
	if logger != nil {
		logger.Info("context summarization started",
			"total_messages", fmt.Sprintf("%d", len(sess.Messages)),
			"keep_recent", fmt.Sprintf("%d", a.summarizeKeepRecent),
		)
	}
	a.logSummary(sess.ChannelID, "Context summarization started")

	// Split messages into old and recent
	old, recent := splitMessages(sess.Messages, a.summarizeKeepRecent)

	if len(old) == 0 {
		if logger != nil {
			logger.Info("context summarization skipped (no old messages to summarize)")
		}
		return nil
	}

	// Build messages for summary LLM call (no tools, just conversation)
	summaryMessages := make([]llm.Message, 0, len(old)+1)
	summaryMessages = append(summaryMessages, llm.NewTextMessage("system", SummaryPrompt))
	for _, msg := range old {
		var smsg llm.Message
		if len(msg.Attachments) > 0 && msg.Content != "" {
			parts := []map[string]interface{}{
				{"type": "text", "text": msg.Content},
			}
			for _, att := range msg.Attachments {
				parts = append(parts, att.ToLLMContentPart())
			}
			contentJSON, _ := json.Marshal(parts)
			smsg = llm.Message{
				Role:    string(msg.Role),
				Content: json.RawMessage(contentJSON),
			}
		} else {
			smsg = llm.NewTextMessage(string(msg.Role), msg.Content)
		}
		smsg.ReasoningContent = msg.ReasoningContent
		smsg.ToolCallID = msg.ToolCallID
		summaryMessages = append(summaryMessages, smsg)
	}

	// Call LLM for summary
	resp, err := a.client.Chat(ctx, summaryMessages, nil, a.maxTokens)
	if err != nil {
		errMsg := fmt.Sprintf("context summarization failed: %v", err)
		a.logAndRecordSummarizationError(logger, sess, errMsg, err)
		return fmt.Errorf("context summarization failed: %w", err)
	}

	summaryText := resp.Content
	if summaryText == "" {
		summaryText = resp.ReasoningContent
	}
	if summaryText == "" {
		errMsg := "context summarization failed: LLM returned empty summary"
		a.logAndRecordSummarizationError(logger, sess, errMsg, fmt.Errorf("empty summary"))
	}

	// Replace old messages with summary, keep recent
	sess.Messages = make([]session.ConversationMessage, 0, len(recent)+1)
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:             session.RoleAssistant,
		Content:          "",
		ReasoningContent: "[Summary of prior conversation]\n" + summaryText,
	})
	sess.Messages = append(sess.Messages, recent...)

	summaryTokens := len(summaryText) / 4
	if logger != nil {
		logger.Info("context summarization complete",
			"old_messages", fmt.Sprintf("%d", len(old)),
			"kept_messages", fmt.Sprintf("%d", len(recent)),
			"summary_tokens", fmt.Sprintf("%d", summaryTokens),
		)
	}
	a.logSummary(sess.ChannelID, fmt.Sprintf("Context summarization complete. Summarized %d messages, kept %d recent.", len(old), len(recent)))

	return nil
}

// logSummary writes a channel log entry for summarization events.
func (a *Agent) logSummary(channelID, message string) {
	if a.channelLogger == nil {
		return
	}
	_ = a.channelLogger.Log(channelID, channellog.Entry{
		Role:    "system",
		Action:  "tool",
		Tool:    "session_summary",
		Message: message,
	})
}

// logAndRecordSummarizationError logs an error during summarization, writes
// a channel log entry, records the failure in the session, and returns the error.
func (a *Agent) logAndRecordSummarizationError(logger *log.Logger, sess *session.Session, errMsg string, err error) {
	if logger != nil {
		logger.Error(errMsg)
	}
	a.logSummary(sess.ChannelID, errMsg)
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:    session.RoleTool,
		Content: errMsg,
	})
}

// totalTokens estimates the total tokens in the system prompt plus all session messages.
func (a *Agent) totalTokens(sess *session.Session, systemPrompt string) int {
	total := len(systemPrompt) / 3
	for _, msg := range sess.Messages {
		total += len(msg.Content) / 3
		total += len(msg.ReasoningContent) / 3
		for _, tc := range msg.ToolCalls {
			total += len(tc.Function.Name) / 2
			total += len(tc.Function.Arguments) / 2
		}
		total += len(msg.ToolCallID) / 2
		// Each image attachment costs ~1000 tokens (image encoder overhead)
		total += len(msg.Attachments) * attachmentTokenCost
	}
	return total
}

// splitMessages splits the message list into old and recent groups.
// The most recent `keepRecent` messages are preserved; everything else is old.
func splitMessages(messages []session.ConversationMessage, keepRecent int) (old, recent []session.ConversationMessage) {
	if keepRecent <= 0 || len(messages) <= keepRecent {
		return messages, nil
	}

	recentStart := len(messages) - keepRecent
	return messages[:recentStart], messages[recentStart:]
}

// parseToolResult parses a tool result string for embedded image attachments.
// If the result is valid JSON containing an "__attachment" key, it extracts the
// attachment and returns the "text" portion as the visible result.
func parseToolResult(result string) (string, []session.ImageAttachment) {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		return result, nil
	}

	attJSON, ok := parsed["__attachment"]
	if !ok {
		return result, nil
	}

	attBytes, err := json.Marshal(attJSON)
	if err != nil {
		return result, nil
	}

	var att session.ImageAttachment
	if err := json.Unmarshal(attBytes, &att); err != nil {
		return result, nil
	}

	// Use the "text" field if present, otherwise return the original result
	if text, ok := parsed["text"].(string); ok {
		return text, []session.ImageAttachment{att}
	}
	return "", []session.ImageAttachment{att}
}

// toLLMMessages converts session messages to LLM API messages, prepending the system prompt.
func (a *Agent) toLLMMessages(sess *session.Session, systemPrompt string) []llm.Message {
	msgs := make([]llm.Message, 0, len(sess.Messages)+1)

	// System prompt is always first
	msgs = append(msgs, llm.NewTextMessage("system", systemPrompt))

	for _, msg := range sess.Messages {
		llmMsg := a.convertMessage(msg)
		msgs = append(msgs, llmMsg)
	}

	return msgs
}

// applyToolCalls applies tool call metadata to an LLM message from a session message.
func applyToolCalls(llmMsg *llm.Message, msg session.ConversationMessage) {
	if len(msg.ToolCalls) > 0 {
		llmMsg.ToolCalls = convertSessionToolCalls(msg.ToolCalls)
	}
	if msg.ToolCallID != "" {
		llmMsg.ToolCallID = msg.ToolCallID
	}
}

// convertMessage converts a session message to an LLM API message.
func (a *Agent) convertMessage(msg session.ConversationMessage) llm.Message {
	// Check if this message has image attachments — if so, use multimodal content
	if len(msg.Attachments) > 0 {
		return a.toMultimodalMessage(msg)
	}

	// Plain text message
	llmMsg := llm.NewTextMessage(string(msg.Role), msg.Content)
	llmMsg.ReasoningContent = msg.ReasoningContent

	applyToolCalls(&llmMsg, msg)

	return llmMsg
}

// toMultimodalMessage converts a message with image attachments to a
// multimodal content-parts message for the LLM vision API.
func (a *Agent) toMultimodalMessage(msg session.ConversationMessage) llm.Message {
	parts := make([]map[string]interface{}, 0, len(msg.Attachments)+1)

	// Add text part first (if any)
	if msg.Content != "" {
		parts = append(parts, map[string]interface{}{
			"type": "text",
			"text": msg.Content,
		})
	}

	// Add image parts
	for _, att := range msg.Attachments {
		parts = append(parts, att.ToLLMContentPart())
	}

	// Marshal the content parts array to raw JSON
	contentJSON, _ := json.Marshal(parts)

	llmMsg := llm.Message{
		Role:             string(msg.Role),
		Content:          contentJSON,
		ReasoningContent: msg.ReasoningContent,
	}

	applyToolCalls(&llmMsg, msg)

	return llmMsg
}
