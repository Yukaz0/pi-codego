package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"pi/pkg/types"
)

type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

func NewClient(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{},
	}
}

func (c *Client) Name() string {
	return "openai"
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	Index    int                `json:"index,omitempty"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openAITool struct {
	Type     string                   `json:"type"`
	Function openAIToolFunctionSchema `json:"function"`
}

type openAIToolFunctionSchema struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Parameters  types.ToolParameterSchema `json:"parameters"`
}

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Tools       []openAITool    `json:"tools,omitempty"`
	Stream      bool            `json:"stream"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (c *Client) Stream(ctx context.Context, req types.CompletionRequest) (<-chan types.StreamEvent, error) {
	out := make(chan types.StreamEvent, 100)

	var messages []openAIMessage

	// Add system prompt if present
	if req.SystemPrompt != "" {
		messages = append(messages, openAIMessage{
			Role:    "system",
			Content: req.SystemPrompt,
		})
	}

	for _, msg := range req.Messages {
		switch msg.Role {
		case types.RoleSystem:
			messages = append(messages, openAIMessage{
				Role:    "system",
				Content: msg.Content,
			})
		case types.RoleUser:
			messages = append(messages, openAIMessage{
				Role:    "user",
				Content: msg.Content,
			})
		case types.RoleAssistant:
			var tcs []openAIToolCall
			for _, tc := range msg.ToolCalls {
				tcs = append(tcs, openAIToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openAIFunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
			messages = append(messages, openAIMessage{
				Role:      "assistant",
				Content:   msg.Content,
				ToolCalls: tcs,
			})
		case types.RoleTool:
			for _, tr := range msg.ToolResults {
				messages = append(messages, openAIMessage{
					Role:       "tool",
					Content:    tr.Content,
					ToolCallID: tr.ToolCallID,
				})
			}
		}
	}

	var tools []openAITool
	for _, t := range req.Tools {
		tools = append(tools, openAITool{
			Type: "function",
			Function: openAIToolFunctionSchema{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	chatReq := openAIChatRequest{
		Model:    req.Model,
		Messages: messages,
		Tools:    tools,
		Stream:   true,
	}
	if req.Temperature > 0 {
		chatReq.Temperature = &req.Temperature
	}
	if req.MaxTokens > 0 {
		chatReq.MaxTokens = &req.MaxTokens
	}

	reqBytes, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("api error (status %d): %s", resp.StatusCode, string(body))
	}

	go func() {
		defer resp.Body.Close()
		defer close(out)

		scanner := bufio.NewScanner(resp.Body)
		const maxSSELine = 10 * 1024 * 1024
		scanner.Buffer(make([]byte, 0, 64*1024), maxSSELine)
		toolCallAccumulator := make(map[int]*types.ToolCall)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				out <- types.StreamEvent{Type: types.EventError, Error: ctx.Err()}
				return
			default:
			}

			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				// Flush any accumulated tool calls
				for _, tc := range toolCallAccumulator {
					out <- types.StreamEvent{
						Type:     types.EventToolCallDone,
						ToolCall: tc,
					}
				}
				out <- types.StreamEvent{Type: types.EventDone}
				return
			}

			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			if chunk.Usage != nil {
				out <- types.StreamEvent{
					Type: types.EventDone,
					Usage: &types.TokenUsage{
						PromptTokens:     chunk.Usage.PromptTokens,
						CompletionTokens: chunk.Usage.CompletionTokens,
						TotalTokens:      chunk.Usage.TotalTokens,
					},
				}
			}

			if len(chunk.Choices) > 0 {
				delta := chunk.Choices[0].Delta
				if delta.Content != "" {
					out <- types.StreamEvent{
						Type:         types.EventContentDelta,
						ContentDelta: delta.Content,
					}
				}

				for _, tc := range delta.ToolCalls {
					idx := tc.Index
					acc, exists := toolCallAccumulator[idx]
					if !exists {
						acc = &types.ToolCall{
							ID:   tc.ID,
							Type: "function",
							Function: types.FunctionCall{
								Name: tc.Function.Name,
							},
						}
						toolCallAccumulator[idx] = acc
					}
					if tc.ID != "" {
						acc.ID = tc.ID
					}
					if tc.Function.Name != "" {
						acc.Function.Name = tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						acc.Function.Arguments += tc.Function.Arguments
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			out <- types.StreamEvent{Type: types.EventError, Error: err}
		}
	}()

	return out, nil
}
