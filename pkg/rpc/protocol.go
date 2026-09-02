package rpc

import "encoding/json"

// RequestType defines RPC request types (stdin -> server)
type RequestType string

const (
	RequestPrompt        RequestType = "prompt"
	RequestAbort         RequestType = "abort"
	RequestSetModel      RequestType = "set_model"
	RequestNewSession    RequestType = "new_session"
	RequestResumeSession RequestType = "resume_session"
	RequestListSessions  RequestType = "list_sessions"
)

// EventType defines RPC event types (server -> stdout)
type EventType string

const (
	EventStart          EventType = "start"
	EventContentDelta   EventType = "content_delta"
	EventThinkingDelta  EventType = "thinking_delta"
	EventToolStart      EventType = "tool_start"
	EventToolEnd        EventType = "tool_end"
	EventTurnStart      EventType = "turn_start"
	EventTurnEnd        EventType = "turn_end"
	EventMessage        EventType = "message"
	EventDone           EventType = "done"
	EventError          EventType = "error"
	EventSessionCreated EventType = "session_created"
	EventAborted        EventType = "aborted"
)

// RPCRequest is a single JSONL line from client to server
type RPCRequest struct {
	Type      RequestType `json:"type"`
	Message   string      `json:"message,omitempty"`
	SessionID string      `json:"session_id,omitempty"`
	Model     string      `json:"model,omitempty"`
	Provider  string      `json:"provider,omitempty"`
	BaseURL   string      `json:"base_url,omitempty"`
	APIKey    string      `json:"api_key,omitempty"`

	// raw preserves unknown fields for forward compatibility
	raw json.RawMessage `json:"-"`
}

// RPCEvent is a single JSONL line from server to client
type RPCEvent struct {
	Type      EventType `json:"type"`
	Delta     string    `json:"delta,omitempty"`
	Content   string    `json:"content,omitempty"`
	Role      string    `json:"role,omitempty"`
	Tool      string    `json:"tool,omitempty"`
	Args      string    `json:"args,omitempty"`
	Result    string    `json:"result,omitempty"`
	IsError   bool      `json:"is_error,omitempty"`
	Error     string    `json:"error,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Model     string    `json:"model,omitempty"`
	Usage     *Usage    `json:"usage,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

func encodeEvent(ev RPCEvent) []byte {
	b, _ := json.Marshal(ev)
	return append(b, '\n')
}
