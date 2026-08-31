package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"pi/pkg/types"
)

// Client for OpenAI Responses API (used by opencode-go muse-spark, gpt-5.6, grok-4.5 etc.)
// Endpoint: {baseUrl}/responses  (not /chat/completions)
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

func NewClient(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://opencode.ai/zen/go/v1"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{},
	}
}

func (c *Client) Name() string { return "responses" }

type responsesInputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesInputItem struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []content
}

type responsesTool struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type responsesRequest struct {
	Model        string          `json:"model"`
	Input        interface{}     `json:"input"` // string or []inputItem
	Instructions string          `json:"instructions,omitempty"`
	Tools        []responsesTool `json:"tools,omitempty"`
	Stream       bool            `json:"stream"`
}

type responsesResponse struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	Status string `json:"status"`
	Output []struct {
		Type    string `json:"type"`
		Role    string `json:"role,omitempty"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content,omitempty"`
		Name      string `json:"name,omitempty"`
		CallID    string `json:"call_id,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"output"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (c *Client) Stream(ctx context.Context, req types.CompletionRequest) (<-chan types.StreamEvent, error) {
	out := make(chan types.StreamEvent, 100)

	// Build input for responses API
	var input interface{}
	if len(req.Messages) == 0 {
		input = req.SystemPrompt
		if input == "" {
			input = "hi"
		}
	} else {
		var items []responsesInputItem
		for _, msg := range req.Messages {
			role := string(msg.Role)
			if role == "tool" {
				// Map tool results to user messages for responses API
				for _, tr := range msg.ToolResults {
					items = append(items, responsesInputItem{
						Role:    "user",
						Content: fmt.Sprintf("Tool %s result: %s", tr.ToolCallID, tr.Content),
					})
				}
				continue
			}
			if role == "assistant" && len(msg.ToolCalls) > 0 {
				// Keep assistant content + tool calls as text
				content := msg.Content
				for _, tc := range msg.ToolCalls {
					content += fmt.Sprintf("\n[Tool call %s: %s(%s)]", tc.ID, tc.Function.Name, tc.Function.Arguments)
				}
				items = append(items, responsesInputItem{Role: "assistant", Content: content})
				continue
			}
			// normal user/assistant
			if role == "assistant" {
				role = "assistant"
			} else if role == "system" {
				role = "system"
			} else {
				role = "user"
			}
			items = append(items, responsesInputItem{Role: role, Content: msg.Content})
		}
		if len(items) == 0 {
			input = req.SystemPrompt
		} else if len(items) == 1 && items[0].Role == "user" {
			// Simplify single user message to string
			if s, ok := items[0].Content.(string); ok {
				input = s
			} else {
				input = items
			}
		} else {
			input = items
		}
	}

	// Build tools for responses API
	var tools []responsesTool
	for _, t := range req.Tools {
		// Convert ToolParameterSchema to map
		params := map[string]interface{}{
			"type": t.Parameters.Type,
		}
		if len(t.Parameters.Properties) > 0 {
			props := make(map[string]interface{})
			for k, v := range t.Parameters.Properties {
				props[k] = map[string]interface{}{
					"type":        v.Type,
					"description": v.Description,
				}
			}
			params["properties"] = props
		}
		if len(t.Parameters.Required) > 0 {
			params["required"] = t.Parameters.Required
		}
		tools = append(tools, responsesTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		})
	}

	// For now use non-streaming and emit as single delta (simpler, avoids SSE parsing complexity)
	// We still respect Stream() interface by emitting delta then done.
	body := responsesRequest{
		Model:        req.Model,
		Input:        input,
		Instructions: req.SystemPrompt,
		Tools:        tools,
		Stream:       false,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal responses request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/responses", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	go func() {
		defer close(out)
		resp, err := c.HTTPClient.Do(httpReq)
		if err != nil {
			out <- types.StreamEvent{Type: types.EventError, Error: err}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)
			out <- types.StreamEvent{Type: types.EventError, Error: fmt.Errorf("responses api error (status %d): %s", resp.StatusCode, string(b))}
			return
		}
		var rResp responsesResponse
		if err := json.NewDecoder(resp.Body).Decode(&rResp); err != nil {
			out <- types.StreamEvent{Type: types.EventError, Error: err}
			return
		}
		if rResp.Error != nil {
			out <- types.StreamEvent{Type: types.EventError, Error: fmt.Errorf("responses error: %s", rResp.Error.Message)}
			return
		}
		// Extract text and tool calls from output
		var fullText string
		var toolCalls []types.ToolCall
		for _, o := range rResp.Output {
			switch o.Type {
			case "message":
				if o.Role == "assistant" {
					for _, c := range o.Content {
						if c.Type == "output_text" {
							fullText += c.Text
						}
					}
				}
			case "function_call":
				toolCalls = append(toolCalls, types.ToolCall{
					ID:   o.CallID,
					Type: "function",
					Function: types.FunctionCall{
						Name:      o.Name,
						Arguments: o.Arguments,
					},
				})
			}
		}
		if fullText != "" {
			out <- types.StreamEvent{Type: types.EventContentDelta, ContentDelta: fullText}
		}
		for _, tc := range toolCalls {
			tcCopy := tc
			out <- types.StreamEvent{Type: types.EventToolCallDone, ToolCall: &tcCopy}
		}
		out <- types.StreamEvent{Type: types.EventDone}
	}()

	return out, nil
}
