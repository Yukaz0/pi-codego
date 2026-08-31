package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"pi/pkg/types"
)

type BashArgs struct {
	Command        string `json:"command"`
	Cwd            string `json:"cwd,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// maxOutputChars caps combined stdout+stderr so huge outputs cannot
// blow up the LLM context window (~30 KB, comparable to read_file's cap).
const maxOutputChars = 30000

type BashTool struct{}

func NewBashTool() *BashTool {
	return &BashTool{}
}

func (t *BashTool) Name() string {
	return "bash"
}

func (t *BashTool) Description() string {
	return "Execute a shell command in bash with optional cwd and timeout."
}

func (t *BashTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: types.ToolParameterSchema{
			Type: "object",
			Properties: map[string]types.PropertyDef{
				"command": {
					Type:        "string",
					Description: "Command string to execute in bash.",
				},
				"cwd": {
					Type:        "string",
					Description: "Optional working directory.",
				},
				"timeout_seconds": {
					Type:        "integer",
					Description: "Optional timeout in seconds (default: 120).",
				},
			},
			Required: []string{"command"},
		},
	}
}

func (t *BashTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args BashArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid bash args: %w", err)
	}

	if args.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	timeout := 120 * time.Second
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", args.Command)
	if args.Cwd != "" {
		cmd.Dir = args.Cwd
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()

	output := stdoutBuf.String()
	if stderrBuf.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += "[STDERR]\n" + stderrBuf.String()
	}

	if cmdCtx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("command timed out after %v", timeout)
	}

	if err != nil {
		if output == "" {
			return "", fmt.Errorf("command exited with error: %w", err)
		}
		return output, fmt.Errorf("command failed: %w", err)
	}

	if output == "" {
		return "(Command completed with no output)", nil
	}

	return truncateOutput(output), nil
}

// truncateOutput keeps the head and tail of oversized output, since both
// ends are usually the most informative parts. Operates on runes so
// multi-byte UTF-8 is never split mid-sequence.
func truncateOutput(s string) string {
	if len(s) <= maxOutputChars {
		return s
	}
	runes := []rune(s)
	half := maxOutputChars / 2
	head := string(runes[:half])
	tail := string(runes[len(runes)-half:])
	return head + "\n\n... [output truncated: " + fmt.Sprintf("%d", len(runes)) +
		" chars total, showing first and last " + fmt.Sprintf("%d", half) + " chars] ...\n\n" + tail
}
