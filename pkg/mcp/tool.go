package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"pi/pkg/types"
)

// MCPTool adapts an MCP server tool to types.Tool
type MCPTool struct {
	client      *Client
	serverName  string
	toolName    string
	description string
	schema      types.ToolParameterSchema
}

func (t *MCPTool) Name() string {
	// prefix to avoid collision: mcp_<server>_<tool>
	return fmt.Sprintf("mcp_%s_%s", sanitize(t.serverName), sanitize(t.toolName))
}

func (t *MCPTool) Description() string {
	if t.description != "" {
		return fmt.Sprintf("[%s] %s", t.serverName, t.description)
	}
	return fmt.Sprintf("MCP tool %s from %s", t.toolName, t.serverName)
}

func (t *MCPTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.schema,
	}
}

func (t *MCPTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t.client == nil {
		return "", fmt.Errorf("mcp client not connected")
	}
	var args map[string]interface{}
	if argsJSON != "" && argsJSON != "{}" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			// treat as raw string if not json
			args = map[string]interface{}{"input": argsJSON}
		}
	}
	if args == nil {
		args = make(map[string]interface{})
	}
	result, err := t.client.CallTool(ctx, t.toolName, args)
	if err != nil {
		return "", err
	}
	// MCP returns content array; flatten to string
	if result == nil {
		return "", nil
	}
	// Try to extract text from content
	if m, ok := result.(map[string]interface{}); ok {
		if content, ok := m["content"]; ok {
			if arr, ok := content.([]interface{}); ok {
				var out string
				for _, c := range arr {
					if cm, ok := c.(map[string]interface{}); ok {
						if cm["type"] == "text" {
							if txt, ok := cm["text"].(string); ok {
								out += txt + "\n"
							}
						} else {
							if b, err := json.Marshal(cm); err == nil {
								out += string(b) + "\n"
							}
						}
					}
				}
				if out != "" {
					return out, nil
				}
			}
		}
		// fallback marshal
		if b, err := json.Marshal(m); err == nil {
			return string(b), nil
		}
	}
	if b, err := json.Marshal(result); err == nil {
		return string(b), nil
	}
	return fmt.Sprintf("%v", result), nil
}

func sanitize(s string) string {
	out := ""
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out += string(r)
		} else {
			out += "_"
		}
	}
	return out
}
