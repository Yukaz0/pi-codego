package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func inDir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func runTodo(t *testing.T, args map[string]any) (string, error) {
	t.Helper()
	b, _ := json.Marshal(args)
	return NewTodoTool().Execute(context.Background(), string(b))
}

func TestTodoToolLifecycle(t *testing.T) {
	dir := t.TempDir()
	inDir(t, dir)

	out, err := runTodo(t, map[string]any{"action": "list"})
	if err != nil || out != "No tasks." {
		t.Fatalf("empty list = %q, %v", out, err)
	}

	if _, err := runTodo(t, map[string]any{"action": "add", "id": "1", "content": "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runTodo(t, map[string]any{"action": "add", "id": "2", "content": "second", "status": "in_progress"}); err != nil {
		t.Fatal(err)
	}
	// duplicate id rejected
	if _, err := runTodo(t, map[string]any{"action": "add", "id": "1", "content": "dup"}); err == nil {
		t.Fatal("expected duplicate id error")
	}
	// second in_progress rejected
	if _, err := runTodo(t, map[string]any{"action": "add", "id": "3", "content": "third", "status": "in_progress"}); err == nil {
		t.Fatal("expected single in_progress error")
	}

	if _, err := runTodo(t, map[string]any{"action": "done", "id": "1"}); err != nil {
		t.Fatal(err)
	}
	out, err = runTodo(t, map[string]any{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[x] 1") || !strings.Contains(out, "[~] 2") {
		t.Fatalf("unexpected list:\n%s", out)
	}

	// persisted on disk
	data, err := os.ReadFile(filepath.Join(dir, ".pi", "todos.json"))
	if err != nil {
		t.Fatal(err)
	}
	var items []todoItem
	if err := json.Unmarshal(data, &items); err != nil || len(items) != 2 {
		t.Fatalf("persisted file bad: %v %v", err, items)
	}

	out, err = runTodo(t, map[string]any{"action": "clear"})
	if err != nil || out != "Cleared all tasks." {
		t.Fatalf("clear = %q, %v", out, err)
	}
	out, _ = runTodo(t, map[string]any{"action": "list"})
	if out != "No tasks." {
		t.Fatalf("after clear: %q", out)
	}
}

func TestTodoToolUpdateAndErrors(t *testing.T) {
	inDir(t, t.TempDir())
	if _, err := runTodo(t, map[string]any{"action": "add", "id": "a", "content": "alpha"}); err != nil {
		t.Fatal(err)
	}
	out, err := runTodo(t, map[string]any{"action": "update", "id": "a", "status": "in_progress", "content": "alpha v2"})
	if err != nil || !strings.Contains(out, "alpha v2") || !strings.Contains(out, "in_progress") {
		t.Fatalf("update = %q, %v", out, err)
	}
	if _, err := runTodo(t, map[string]any{"action": "update", "id": "zzz", "status": "done"}); err == nil {
		t.Fatal("expected not-found error")
	}
	if _, err := runTodo(t, map[string]any{"action": "bogus"}); err == nil {
		t.Fatal("expected unknown action error")
	}
	if _, err := runTodo(t, map[string]any{"action": "add", "id": "b", "content": "x", "status": "nope"}); err == nil {
		t.Fatal("expected invalid status error")
	}
}
