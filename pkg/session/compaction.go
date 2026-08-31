package session

import (
	"context"
	"fmt"
	"strings"

	"pi/pkg/types"
)

type Compactor struct {
	MaxMessages int
	// MaxTokens is an optional token budget (rough estimate: chars/4).
	// When set and exceeded, compaction triggers even below MaxMessages.
	MaxTokens int
	Provider  types.Provider
	Model     string
	// ExtraInstructions biases the LLM summary when set (manual /compact args).
	ExtraInstructions string
}

func NewCompactor(maxMessages int, p types.Provider, model string) *Compactor {
	if maxMessages <= 0 {
		maxMessages = 20
	}
	return &Compactor{
		MaxMessages: maxMessages,
		Provider:    p,
		Model:       model,
	}
}

// EstimateTokens is a rough chars/4 heuristic used for compaction triggers.
func EstimateTokens(msgs []types.Message) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content) / 4
		for _, tc := range m.ToolCalls {
			total += len(tc.Function.Name) / 4
			total += len(tc.Function.Arguments) / 4
		}
	}
	return total
}

// CompactIfNeeded checks if linear history exceeds the message or token
// budget and compacts the older portion. When a Provider is configured the
// summary is produced by the LLM (Pi-style); otherwise it falls back to a
// deterministic preview digest.
func (c *Compactor) CompactIfNeeded(ctx context.Context, history []types.Message) ([]types.Message, bool, error) {
	overMsgs := len(history) > c.MaxMessages
	overTokens := c.MaxTokens > 0 && EstimateTokens(history) > c.MaxTokens
	if !overMsgs && !overTokens {
		return history, false, nil
	}
	return c.Compact(ctx, history)
}

// Compact forces compaction regardless of thresholds (manual /compact).
func (c *Compactor) Compact(ctx context.Context, history []types.Message) ([]types.Message, bool, error) {
	if len(history) <= 4 {
		return history, false, nil
	}
	splitIdx := len(history) - (c.MaxMessages / 2)
	if splitIdx < 1 {
		splitIdx = len(history) / 2
	}
	if splitIdx > len(history)-2 {
		splitIdx = len(history) - 2
	}
	oldMessages := history[:splitIdx]
	recentMessages := history[splitIdx:]

	var summary string
	if c.Provider != nil {
		s, err := c.summarizeWithLLM(ctx, oldMessages)
		if err == nil && strings.TrimSpace(s) != "" {
			summary = s
		} else if err != nil {
			// fall back to deterministic digest on provider failure
			summary = digestMessages(oldMessages)
		} else {
			summary = digestMessages(oldMessages)
		}
	} else {
		summary = digestMessages(oldMessages)
	}

	compactedMessage := types.Message{
		Role:      types.RoleSystem,
		Content:   "Summary of earlier conversation:\n" + summary,
		CreatedAt: oldMessages[len(oldMessages)-1].CreatedAt,
	}

	result := append([]types.Message{compactedMessage}, recentMessages...)
	return result, true, nil
}

const summarizePrompt = `You are compacting an earlier portion of a coding-agent conversation.
Write a dense, faithful summary that lets the agent continue seamlessly.
Preserve: user goals and constraints, file paths touched, key decisions,
tool results that matter (test outcomes, errors), and any open questions.
Do NOT drop numeric IDs, branch names, or error messages. Output only the summary.`

func (c *Compactor) summarizeWithLLM(ctx context.Context, msgs []types.Message) (string, error) {
	// Feed the old conversation as user-role transcript to a single completion.
	var transcript strings.Builder
	for _, m := range msgs {
		content := m.Content
		if len(content) > 2000 {
			content = content[:2000] + "…[truncated]"
		}
		transcript.WriteString(fmt.Sprintf("<%s>\n%s\n", m.Role, content))
		for _, tc := range m.ToolCalls {
			args := tc.Function.Arguments
			if len(args) > 500 {
				args = args[:500] + "…"
			}
			transcript.WriteString(fmt.Sprintf("<tool_call> %s %s\n", tc.Function.Name, args))
		}
	}

	req := types.CompletionRequest{
		Model:        c.Model,
		SystemPrompt: summarizePrompt,
		Messages:     []types.Message{{Role: types.RoleUser, Content: transcript.String()}},
		MaxTokens:    1024,
	}
	if extra := strings.TrimSpace(c.ExtraInstructions); extra != "" {
		req.Messages[0].Content += "\n\nAdditional instructions for the summary: " + extra
	}
	events, err := c.Provider.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for ev := range events {
		switch ev.Type {
		case types.EventContentDelta:
			sb.WriteString(ev.ContentDelta)
		case types.EventError:
			if ev.Error != nil {
				return "", ev.Error
			}
		}
	}
	return sb.String(), nil
}

// digestMessages is the deterministic fallback: role + preview per message.
func digestMessages(msgs []types.Message) string {
	var sb strings.Builder
	for _, msg := range msgs {
		contentPreview := msg.Content
		if len(contentPreview) > 100 {
			contentPreview = contentPreview[:100] + "..."
		}
		sb.WriteString(fmt.Sprintf("- [%s]: %s\n", msg.Role, contentPreview))
	}
	return sb.String()
}
