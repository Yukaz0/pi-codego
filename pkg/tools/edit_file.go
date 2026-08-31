package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"pi/pkg/types"
)

type EditFileArgs struct {
	Path               string `json:"path"`
	TargetContent      string `json:"target_content"`
	ReplacementContent string `json:"replacement_content"`
	AllowMultiple      bool   `json:"allow_multiple,omitempty"`
}

type EditFileTool struct{}

func NewEditFileTool() *EditFileTool {
	return &EditFileTool{}
}

func (t *EditFileTool) Name() string {
	return "edit_file"
}

func (t *EditFileTool) Description() string {
	return "Replace target_content with replacement_content in a file. target_content must match exactly."
}

func (t *EditFileTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: types.ToolParameterSchema{
			Type: "object",
			Properties: map[string]types.PropertyDef{
				"path": {
					Type:        "string",
					Description: "Path to the file to modify.",
				},
				"target_content": {
					Type:        "string",
					Description: "Exact string to find and replace.",
				},
				"replacement_content": {
					Type:        "string",
					Description: "Exact replacement string.",
				},
				"allow_multiple": {
					Type:        "boolean",
					Description: "Set true to replace all occurrences instead of requiring a unique match.",
				},
			},
			Required: []string{"path", "target_content", "replacement_content"},
		},
	}
}

func (t *EditFileTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args EditFileArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid edit_file args: %w", err)
	}

	if args.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if args.TargetContent == "" {
		return "", fmt.Errorf("target_content cannot be empty")
	}

	contentBytes, err := os.ReadFile(args.Path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", args.Path, err)
	}

	content := string(contentBytes)
	count := strings.Count(content, args.TargetContent)
	if count == 0 {
		return "", fmt.Errorf("target_content not found in %s", args.Path)
	}
	if count > 1 && !args.AllowMultiple {
		return "", fmt.Errorf("found %d occurrences of target_content in %s. Set allow_multiple=true or provide more context", count, args.Path)
	}

	var newContent string
	if args.AllowMultiple {
		newContent = strings.ReplaceAll(content, args.TargetContent, args.ReplacementContent)
	} else {
		newContent = strings.Replace(content, args.TargetContent, args.ReplacementContent, 1)
	}

	if err := os.WriteFile(args.Path, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write edited file %s: %w", args.Path, err)
	}

	return fmt.Sprintf("Successfully replaced %d occurrence(s) in %s", count, args.Path), nil
}
