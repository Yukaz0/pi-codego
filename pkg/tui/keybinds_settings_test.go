package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKeyOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	settings := map[string]any{
		"defaultModel": "x",
		"keybindings": map[string]string{
			"pasteImage":  "ctrl+b",
			"modelPicker": "f2",
			"bogusAction": "ctrl+z",
		},
	}
	data, _ := json.Marshal(settings)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	alias := loadKeyOverrides()
	if alias["ctrl+b"] != "ctrl+v" {
		t.Fatalf("ctrl+b alias = %q, want ctrl+v", alias["ctrl+b"])
	}
	if alias["f2"] != "ctrl+o" {
		t.Fatalf("f2 alias = %q, want ctrl+o", alias["f2"])
	}
	if _, ok := alias["ctrl+z"]; ok {
		t.Fatal("unknown action should be ignored")
	}
}

func TestLoadKeyOverridesMissingFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := loadKeyOverrides(); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}
