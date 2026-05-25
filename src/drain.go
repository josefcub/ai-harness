package main

import (
	"github.com/agent-project/harness/log"
	"github.com/agent-project/harness/queue"
	"github.com/agent-project/harness/session"
)

// drainPending drains all pending messages from the queue into session files,
// preventing message loss during shutdown. Each message is appended to its
// channel's session without calling the LLM. Errors are logged individually
// and do not abort the drain. Returns the count of successfully drained messages.
// If logger is nil, logging is skipped.
func drainPending(q *queue.Queue, sessions *session.Manager, logger *log.Logger) int {
	pending := q.Pending()
	var count int
	for _, msg := range pending {
		if err := sessions.DrainAndSave(msg.ChannelID, msg.MessageText, msg.ImageAttachment); err != nil {
			if logger != nil {
				logger.Error("failed to drain message to session",
					"channel", msg.ChannelID,
					"error", err.Error(),
				)
			}
		} else {
			if logger != nil {
				logger.Info("drained pending message to session", "channel", msg.ChannelID)
			}
			count++
		}
	}
	return count
}
