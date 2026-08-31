package tui

import "testing"

func TestSlashSuggestions(t *testing.T) {
	m := Model{}
	got := m.slashSuggestions("/co")
	names := map[string]bool{}
	for _, s := range got {
		names[s.Name] = true
	}
	if !names["compact"] || !names["copy"] {
		t.Fatalf("expected compact/copy/clear, got %v", got)
	}
	for _, s := range got {
		if s.Name == "model" {
			t.Fatalf("model should not match /co")
		}
	}
	if m.slashSuggestions("hello") != nil {
		t.Fatal("non-slash must return nil")
	}
	if m.slashSuggestions("/model gpt") != nil {
		t.Fatal("with args must return nil")
	}
	if len(m.slashSuggestions("/")) != len(slashCommands) {
		t.Fatal("/ should list all commands")
	}
}
