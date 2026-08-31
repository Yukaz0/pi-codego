package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runEdit(t *testing.T, argsJSON string) (string, error) {
	t.Helper()
	return NewEditFileTool().Execute(context.Background(), argsJSON)
}

func TestEditFileExactReplace(t *testing.T) {
	path := writeTempFile(t, "hello world\n")
	out, err := runEdit(t, `{"path":"`+path+`","target_content":"world","replacement_content":"Go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hello Go\n" {
		t.Errorf("file = %q, want %q", got, "hello Go\n")
	}
	if !strings.Contains(out, "Successfully replaced") {
		t.Errorf("result message = %q", out)
	}
}

func TestEditFileAmbiguousRequiresFlag(t *testing.T) {
	path := writeTempFile(t, "aaa\naaa\n")
	args := `{"path":"` + path + `","target_content":"aaa","replacement_content":"b"}`
	if _, err := runEdit(t, args); err == nil {
		t.Fatal("expected error for multiple occurrences without allow_multiple")
	}
	if _, err := runEdit(t, args[:len(args)-1]+`,"allow_multiple":true}`); err != nil {
		t.Fatalf("allow_multiple should succeed: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "b\nb\n" {
		t.Errorf("file = %q, want %q", got, "b\nb\n")
	}
}

func TestEditFileTargetNotFound(t *testing.T) {
	path := writeTempFile(t, "hello\n")
	_, err := runEdit(t, `{"path":"`+path+`","target_content":"nope","replacement_content":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want not-found", err)
	}
}

func TestEditFileValidation(t *testing.T) {
	if _, err := runEdit(t, `{bad json}`); err == nil {
		t.Error("expected invalid-args error")
	}
	path := writeTempFile(t, "x")
	if _, err := runEdit(t, `{"path":"","target_content":"a","replacement_content":"b"}`); err == nil {
		t.Error("expected missing-path error")
	}
	if _, err := runEdit(t, `{"path":"`+path+`","target_content":"","replacement_content":"b"}`); err == nil {
		t.Error("expected empty-target error")
	}
	if _, err := runEdit(t, `{"path":"/nonexistent/dir/f.txt","target_content":"a","replacement_content":"b"}`); err == nil {
		t.Error("expected read-file error")
	}
}
