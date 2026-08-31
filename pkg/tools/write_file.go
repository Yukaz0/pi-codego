package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"pi/pkg/types"
)

type WriteFileArgs struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Overwrite bool   `json:"overwrite,omitempty"`
}

type WriteFileTool struct{}

func NewWriteFileTool() *WriteFileTool {
	return &WriteFileTool{}
}

func (t *WriteFileTool) Name() string {
	return "write_file"
}

func (t *WriteFileTool) Description() string {
	return "Create a new file or overwrite an existing file with the specified content."
}

func (t *WriteFileTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: types.ToolParameterSchema{
			Type: "object",
			Properties: map[string]types.PropertyDef{
				"path": {
					Type:        "string",
					Description: "Absolute or relative path to the file to create/overwrite.",
				},
				"content": {
					Type:        "string",
					Description: "Text content to write into the file.",
				},
				"overwrite": {
					Type:        "boolean",
					Description: "Set true to overwrite if file already exists.",
				},
			},
			Required: []string{"path", "content"},
		},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args WriteFileArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid write_file args: %w", err)
	}

	if args.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	if _, err := os.Stat(args.Path); err == nil && !args.Overwrite {
		return "", fmt.Errorf("file %s already exists. Set overwrite=true to replace it", args.Path)
	}

	dir := filepath.Dir(args.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(args.Path, []byte(args.Content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", args.Path, err)
	}

	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(args.Content), args.Path), nil
}
