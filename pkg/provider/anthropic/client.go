package anthropic

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
		baseURL = "https://api.anthropic.com/v1"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{},
	}
}

func (c *Client) Name() string {
	return "anthropic"
}

type anthropicContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	Content   string         `json:"content,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Source    map[string]any `json:"source,omitempty"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicTool struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	InputSchema types.ToolParameterSchema `json:"input_schema"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	Messages  []anthropicMessage `json:"messages"`
	System    string             `json:"system,omitempty"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream"`
}

func (c *Client) Stream(ctx context.Context, req types.CompletionRequest) (<-chan types.StreamEvent, error) {
	out := make(chan types.StreamEvent, 100)

	var messages []anthropicMessage

	for _, msg := range req.Messages {
		switch msg.Role {
		case types.RoleUser:
			blocks := []anthropicContentBlock{}
			if msg.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: msg.Content})
			}
			for _, img := range msg.Images {
				blocks = append(blocks, anthropicContentBlock{
					Type: "image",
					Source: map[string]any{
						"type":       "base64",
						"media_type": img.MediaType,
						"data":       img.Data,
					},
				})
			}
			if len(blocks) == 0 {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: ""})
			}
			messages = append(messages, anthropicMessage{Role: "user", Content: blocks})
		case types.RoleAssistant:
			var blocks []anthropicContentBlock
			if msg.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				var inputMap map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &inputMap)
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: inputMap,
				})
			}
			messages = append(messages, anthropicMessage{
				Role:    "assistant",
				Content: blocks,
			})
		case types.RoleTool:
			var blocks []anthropicContentBlock
			for _, tr := range msg.ToolResults {
				blocks = append(blocks, anthropicContentBlock{
					Type:      "tool_result",
					ToolUseID: tr.ToolCallID,
					Content:   tr.Content,
				})
			}
			messages = append(messages, anthropicMessage{
				Role:    "user",
				Content: blocks,
			})
		}
	}

	var tools []anthropicTool
	for _, t := range req.Tools {
		tools = append(tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	anthReq := anthropicRequest{
		Model:     req.Model,
		Messages:  messages,
		System:    req.SystemPrompt,
		Tools:     tools,
		MaxTokens: maxTokens,
		Stream:    true,
	}

	reqBytes, err := json.Marshal(anthReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal anthropic request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/messages", bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic api error (status %d): %s", resp.StatusCode, string(body))
	}

	go func() {
		defer resp.Body.Close()
		defer close(out)

		scanner := bufio.NewScanner(resp.Body)
		const maxSSELine = 10 * 1024 * 1024
		scanner.Buffer(make([]byte, 0, 64*1024), maxSSELine)
		var currentToolCall *types.ToolCall

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				out <- types.StreamEvent{Type: types.EventError, Error: ctx.Err()}
				return
			default:
			}

			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			var event map[string]any
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			eventType, _ := event["type"].(string)
			switch eventType {
			case "content_block_start":
				if cb, ok := event["content_block"].(map[string]any); ok {
					if cb["type"] == "tool_use" {
						currentToolCall = &types.ToolCall{
							ID:   cb["id"].(string),
							Type: "function",
							Function: types.FunctionCall{
								Name: cb["name"].(string),
							},
						}
					}
				}
			case "content_block_delta":
				if delta, ok := event["delta"].(map[string]any); ok {
					deltaType, _ := delta["type"].(string)
					if deltaType == "text_delta" {
						out <- types.StreamEvent{
							Type:         types.EventContentDelta,
							ContentDelta: delta["text"].(string),
						}
					} else if deltaType == "input_json_delta" && currentToolCall != nil {
						currentToolCall.Function.Arguments += delta["partial_json"].(string)
					}
				}
			case "content_block_stop":
				if currentToolCall != nil {
					out <- types.StreamEvent{
						Type:     types.EventToolCallDone,
						ToolCall: currentToolCall,
					}
					currentToolCall = nil
				}
			case "message_stop":
				out <- types.StreamEvent{Type: types.EventDone}
				return
			}
		}

		if err := scanner.Err(); err != nil {
			out <- types.StreamEvent{Type: types.EventError, Error: err}
		}
	}()

	return out, nil
}
