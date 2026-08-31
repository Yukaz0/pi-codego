package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pi/pkg/types"
)

// TodoTool maintains a lightweight task list for multi-step work, persisted
// in .pi/todos.json under the working directory.
type TodoTool struct{}

func NewTodoTool() *TodoTool { return &TodoTool{} }

func (t *TodoTool) Name() string { return "todo" }

func (t *TodoTool) Description() string {
	return "Maintain a task list for multi-step work. Actions: list (show all), add {id,content,status}, update {id,status|content}, done {id} (mark completed), clear (remove all). Statuses: pending, in_progress, completed, cancelled. Only one item may be in_progress at a time."
}

func (t *TodoTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: types.ToolParameterSchema{
			Type: "object",
			Properties: map[string]types.PropertyDef{
				"action": {Type: "string", Description: "One of: list, add, update, done, clear"},
				"id":     {Type: "string", Description: "Task id (for add/update/done)"},
				"content": {Type: "string", Description: "Task description (for add/update)"},
				"status":  {Type: "string", Description: "pending|in_progress|completed|cancelled (for add/update)"},
			},
			Required: []string{"action"},
		},
	}
}

type todoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

var todoStatuses = map[string]bool{
	"pending": true, "in_progress": true, "completed": true, "cancelled": true,
}

func todoPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot resolve working directory: %w", err)
	}
	return filepath.Join(cwd, ".pi", "todos.json"), nil
}

func loadTodos(path string) ([]todoItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var items []todoItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("corrupt todos file (%s): %w", path, err)
	}
	return items, nil
}

func saveTodos(path string, items []todoItem) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func renderTodos(items []todoItem) string {
	if len(items) == 0 {
		return "No tasks."
	}
	var sb strings.Builder
	for _, it := range items {
		mark := "[ ]"
		switch it.Status {
		case "in_progress":
			mark = "[~]"
		case "completed":
			mark = "[x]"
		case "cancelled":
			mark = "[-]"
		}
		sb.WriteString(fmt.Sprintf("%s %s — %s (%s)\n", mark, it.ID, it.Content, it.Status))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (t *TodoTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Action  string `json:"action"`
		ID      string `json:"id"`
		Content string `json:"content"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	path, err := todoPath()
	if err != nil {
		return "", err
	}
	items, err := loadTodos(path)
	if err != nil {
		return "", err
	}

	switch strings.ToLower(strings.TrimSpace(args.Action)) {
	case "list", "":
		return renderTodos(items), nil

	case "clear":
		if err := saveTodos(path, nil); err != nil {
			return "", err
		}
		return "Cleared all tasks.", nil

	case "done":
		if args.ID == "" {
			return "", fmt.Errorf("id is required for done")
		}
		found := false
		for i := range items {
			if items[i].ID == args.ID {
				items[i].Status = "completed"
				found = true
			}
		}
		if !found {
			return "", fmt.Errorf("task %q not found", args.ID)
		}
		if err := saveTodos(path, items); err != nil {
			return "", err
		}
		return renderTodos(items), nil

	case "add":
		if args.ID == "" || strings.TrimSpace(args.Content) == "" {
			return "", fmt.Errorf("id and content are required for add")
		}
		status := args.Status
		if status == "" {
			status = "pending"
		}
		if !todoStatuses[status] {
			return "", fmt.Errorf("invalid status %q (pending|in_progress|completed|cancelled)", status)
		}
		for _, it := range items {
			if it.ID == args.ID {
				return "", fmt.Errorf("task %q already exists (use update)", args.ID)
			}
		}
		if status == "in_progress" {
			for _, it := range items {
				if it.Status == "in_progress" {
					return "", fmt.Errorf("task %q is already in_progress; only one at a time", it.ID)
				}
			}
		}
		items = append(items, todoItem{ID: args.ID, Content: args.Content, Status: status})
		if err := saveTodos(path, items); err != nil {
			return "", err
		}
		return renderTodos(items), nil

	case "update":
		if args.ID == "" {
			return "", fmt.Errorf("id is required for update")
		}
		found := false
		for i := range items {
			if items[i].ID != args.ID {
				continue
			}
			found = true
			if args.Content != "" {
				items[i].Content = args.Content
			}
			if args.Status != "" {
				if !todoStatuses[args.Status] {
					return "", fmt.Errorf("invalid status %q (pending|in_progress|completed|cancelled)", args.Status)
				}
				if args.Status == "in_progress" {
					for j := range items {
						if items[j].Status == "in_progress" && items[j].ID != args.ID {
							return "", fmt.Errorf("task %q is already in_progress; only one at a time", items[j].ID)
						}
					}
				}
				items[i].Status = args.Status
			}
		}
		if !found {
			return "", fmt.Errorf("task %q not found", args.ID)
		}
		if err := saveTodos(path, items); err != nil {
			return "", err
		}
		return renderTodos(items), nil

	default:
		return "", fmt.Errorf("unknown action %q (list|add|update|done|clear)", args.Action)
	}
}
