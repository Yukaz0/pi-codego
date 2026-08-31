package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"pi/pkg/types"
)

type ReadFileArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

type ReadFileTool struct{}

func NewReadFileTool() *ReadFileTool {
	return &ReadFileTool{}
}

func (t *ReadFileTool) Name() string {
	return "read_file"
}

func (t *ReadFileTool) Description() string {
	return "Read the contents of a file with optional 1-indexed line ranges (start_line, end_line)."
}

func (t *ReadFileTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: types.ToolParameterSchema{
			Type: "object",
			Properties: map[string]types.PropertyDef{
				"path": {
					Type:        "string",
					Description: "Absolute or relative path to the file to read.",
				},
				"start_line": {
					Type:        "integer",
					Description: "Optional starting line number (1-indexed, inclusive).",
				},
				"end_line": {
					Type:        "integer",
					Description: "Optional ending line number (1-indexed, inclusive).",
				},
			},
			Required: []string{"path"},
		},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args ReadFileArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid read_file args: %w", err)
	}

	if args.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	file, err := os.Open(args.Path)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", args.Path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Allow large lines up to 1MB (default 64KB is too small for minified files)
	const maxLineSize = 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxLineSize)
	var lines []string
	currentLine := 1
	// Safety cap: max 2000 lines per read to avoid OOM
	const maxLines = 2000

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		if (args.StartLine <= 0 || currentLine >= args.StartLine) &&
			(args.EndLine <= 0 || currentLine <= args.EndLine) {
			lines = append(lines, fmt.Sprintf("%4d | %s", currentLine, scanner.Text()))
			if len(lines) >= maxLines {
				lines = append(lines, fmt.Sprintf("... (truncated at %d lines, use start_line/end_line to view more)", maxLines))
				break
			}
		}
		currentLine++
		// If we passed end_line, stop early
		if args.EndLine > 0 && currentLine > args.EndLine {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading file %s: %w", args.Path, err)
	}

	if len(lines) == 0 {
		return fmt.Sprintf("(File %s is empty or specified range contains no lines)", args.Path), nil
	}

	return strings.Join(lines, "\n"), nil
}
