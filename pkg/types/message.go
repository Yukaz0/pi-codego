package types

import (
	"encoding/json"
	"time"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // e.g. "function"
	Function FunctionCall `json:"function"`
}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
}

type Message struct {
	ID          string            `json:"id,omitempty"`
	Role        Role              `json:"role"`
	Content     string            `json:"content"`
	ToolCalls   []ToolCall        `json:"tool_calls,omitempty"`
	ToolResults []ToolResult      `json:"tool_results,omitempty"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

func NewSystemMessage(content string) Message {
	return Message{
		Role:      RoleSystem,
		Content:   content,
		CreatedAt: time.Now(),
	}
}

func NewUserMessage(content string) Message {
	return Message{
		Role:      RoleUser,
		Content:   content,
		CreatedAt: time.Now(),
	}
}

func NewAssistantMessage(content string, toolCalls []ToolCall) Message {
	return Message{
		Role:      RoleAssistant,
		Content:   content,
		ToolCalls: toolCalls,
		CreatedAt: time.Now(),
	}
}

func NewToolResultMessage(toolCallID, content string, isError bool) Message {
	return Message{
		Role: RoleTool,
		ToolResults: []ToolResult{
			{
				ToolCallID: toolCallID,
				Content:    content,
				IsError:    isError,
			},
		},
		CreatedAt: time.Now(),
	}
}

func (m Message) ToJSON() (string, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
