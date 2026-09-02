package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"

	"pi/pkg/types"
)

// Regression: 'y' while typing must reach the textarea (like '?'), not be
// swallowed by the copy-last-answer shortcut.
func TestYKeyReachesTextareaWhileTyping(t *testing.T) {
	m := NewModel(nil)
	m.textarea.SetValue("he")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	um := updated.(*Model)
	if got := um.textarea.Value(); got != "hey" {
		t.Fatalf("textarea = %q, want %q (y should be inserted while typing)", got, "hey")
	}
}

// Regression: 'y' when idle with empty input is a copy command and must NOT
// insert a character into the textarea.
func TestYKeyCopiesWhenIdleEmpty(t *testing.T) {
	m := NewModel(nil)
	m.history = append(m.history, types.NewAssistantMessage("some answer", nil))
	m.textarea.SetValue("")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	um := updated.(*Model)
	if got := um.textarea.Value(); got != "" {
		t.Fatalf("textarea = %q, want empty (copy command when idle)", got)
	}

	// a user-visible notice must confirm the copy happened
	found := false
	for _, n := range um.notices {
		if strings.Contains(n.text, "copied last answer") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected a 'copied last answer' notice after copying")
	}
}
