package gemini

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
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{},
	}
}

func (c *Client) Name() string {
	return "gemini"
}

type geminiPart struct {
	Text             string              `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type geminiFuncResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiReq struct {
	Contents          []geminiContent `json:"contents"`
	SystemInstruction *geminiContent  `json:"system_instruction,omitempty"`
}

type geminiResponseChunk struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
			Role  string       `json:"role"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
}

func (c *Client) Stream(ctx context.Context, req types.CompletionRequest) (<-chan types.StreamEvent, error) {
	out := make(chan types.StreamEvent, 100)

	var contents []geminiContent

	for _, msg := range req.Messages {
		switch msg.Role {
		case types.RoleUser:
			contents = append(contents, geminiContent{
				Role: "user",
				Parts: []geminiPart{
					{Text: msg.Content},
				},
			})
		case types.RoleAssistant:
			var parts []geminiPart
			if msg.Content != "" {
				parts = append(parts, geminiPart{Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				var argsMap map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &argsMap)
				parts = append(parts, geminiPart{
					FunctionCall: &geminiFunctionCall{
						Name: tc.Function.Name,
						Args: argsMap,
					},
				})
			}
			contents = append(contents, geminiContent{
				Role:  "model",
				Parts: parts,
			})
		case types.RoleTool:
			for _, tr := range msg.ToolResults {
				contents = append(contents, geminiContent{
					Role: "function",
					Parts: []geminiPart{
						{
							FunctionResponse: &geminiFuncResponse{
								Name: tr.ToolCallID,
								Response: map[string]any{
									"result": tr.Content,
								},
							},
						},
					},
				})
			}
		}
	}

	gReq := geminiReq{
		Contents: contents,
	}

	if req.SystemPrompt != "" {
		gReq.SystemInstruction = &geminiContent{
			Parts: []geminiPart{
				{Text: req.SystemPrompt},
			},
		}
	}

	reqBytes, err := json.Marshal(gReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gemini request: %w", err)
	}

	modelName := strings.TrimPrefix(req.Model, "models/")
	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", c.BaseURL, modelName, c.APIKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("gemini api error (status %d): %s", resp.StatusCode, string(body))
	}

	go func() {
		defer resp.Body.Close()
		defer close(out)

		scanner := bufio.NewScanner(resp.Body)
		const maxSSELine = 10 * 1024 * 1024
		scanner.Buffer(make([]byte, 0, 64*1024), maxSSELine)
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
			var chunk geminiResponseChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			for _, candidate := range chunk.Candidates {
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						out <- types.StreamEvent{
							Type:         types.EventContentDelta,
							ContentDelta: part.Text,
						}
					}
					if part.FunctionCall != nil {
						argsBytes, _ := json.Marshal(part.FunctionCall.Args)
						out <- types.StreamEvent{
							Type: types.EventToolCallDone,
							ToolCall: &types.ToolCall{
								ID:   part.FunctionCall.Name,
								Type: "function",
								Function: types.FunctionCall{
									Name:      part.FunctionCall.Name,
									Arguments: string(argsBytes),
								},
							},
						}
					}
				}
			}
		}

		out <- types.StreamEvent{Type: types.EventDone}
	}()

	return out, nil
}
