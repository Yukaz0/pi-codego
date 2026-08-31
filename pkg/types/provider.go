package types

import "context"

type StreamEventType string

const (
	EventContentDelta  StreamEventType = "content_delta"
	EventThinkingDelta StreamEventType = "thinking_delta"
	EventToolCallDelta StreamEventType = "tool_call_delta"
	EventToolCallDone  StreamEventType = "tool_call_done"
	EventDone          StreamEventType = "done"
	EventError         StreamEventType = "error"
)

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// CacheReadTokens / CacheWriteTokens are Anthropic prompt-cache stats.
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

type StreamEvent struct {
	Type          StreamEventType `json:"type"`
	ContentDelta  string          `json:"content_delta,omitempty"`
	ThinkingDelta string          `json:"thinking_delta,omitempty"`
	ToolCall      *ToolCall       `json:"tool_call,omitempty"`
	Usage         *TokenUsage     `json:"usage,omitempty"`
	Error         error           `json:"error,omitempty"`
}

type CompletionRequest struct {
	Model         string           `json:"model"`
	SystemPrompt  string           `json:"system_prompt,omitempty"`
	Messages      []Message        `json:"messages"`
	Tools         []ToolDefinition `json:"tools,omitempty"`
	MaxTokens     int              `json:"max_tokens,omitempty"`
	Temperature   float64          `json:"temperature,omitempty"`
	ThinkingLevel string           `json:"thinking_level,omitempty"` // low, medium, high
}

type Provider interface {
	Name() string
	Stream(ctx context.Context, req CompletionRequest) (<-chan StreamEvent, error)
}
