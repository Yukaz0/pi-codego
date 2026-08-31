package tools

import (
	"context"
	"strings"
	"testing"
	"unicode"
)

func runBash(t *testing.T, argsJSON string) (string, error) {
	t.Helper()
	return NewBashTool().Execute(context.Background(), argsJSON)
}

func TestBashBasicOutput(t *testing.T) {
	out, err := runBash(t, `{"command":"echo hello bash"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello bash") {
		t.Errorf("output missing expected text: %q", out)
	}
}

func TestBashMergesStderr(t *testing.T) {
	out, err := runBash(t, `{"command":"echo out; echo err 1>&2"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "out") || !strings.Contains(out, "[STDERR]") || !strings.Contains(out, "err") {
		t.Errorf("stderr not merged properly:\n%s", out)
	}
}

func TestBashEmptyOutput(t *testing.T) {
	out, err := runBash(t, `{"command":"true"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "(Command completed with no output)" {
		t.Errorf("empty-output sentinel = %q", out)
	}
}

func TestBashTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	_, err := runBash(t, `{"command":"sleep 5","timeout_seconds":1}`)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v, want timeout", err)
	}
}

func TestBashTruncatesHugeOutput(t *testing.T) {
	out, err := runBash(t, `{"command":"yes x | head -c 40000"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) > maxOutputChars+200 { // small slack for the notice
		t.Errorf("output not truncated: %d chars", len(out))
	}
	if !strings.Contains(out, "[output truncated") {
		t.Errorf("truncation notice missing")
	}
	// Tail of the original output must survive truncation.
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "x") {
		t.Errorf("tail of output lost")
	}
}

func TestBashTruncationKeepsUTF8Intact(t *testing.T) {
	// Multi-byte runes (é = 2 bytes) straddling the cut boundary.
	out := truncateOutput(strings.Repeat("é", 20000))
	for _, r := range out {
		if r == unicode.ReplacementChar {
			t.Fatal("output contains U+FFFD: rune split mid-sequence")
		}
	}
}

func TestBashInvalidArgs(t *testing.T) {
	if _, err := runBash(t, `{not json}`); err == nil {
		t.Error("expected invalid-args error")
	}
	if _, err := runBash(t, `{"command":""}`); err == nil {
		t.Error("expected empty-command error")
	}
}
