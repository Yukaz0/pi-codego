package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"

	"pi/pkg/types"
)

// Regression: bare 'y' while typing must reach the textarea (like '?'), not be
// swallowed by a copy shortcut.
func TestYKeyReachesTextareaWhileTyping(t *testing.T) {
	m := NewModel(nil)
	m.textarea.SetValue("he")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	um := updated.(*Model)
	if got := um.textarea.Value(); got != "hey" {
		t.Fatalf("textarea = %q, want %q (y should be inserted while typing)", got, "hey")
	}
}

// Regression: bare 'y' must type a character even when the input is empty, so a
// message like "yes" or any word starting with 'y' can be typed. It must NOT be
// hijacked into a copy command.
func TestYKeyAlwaysTypesWhenIdleEmpty(t *testing.T) {
	m := NewModel(nil)
	m.history = append(m.history, types.NewAssistantMessage("some answer", nil))
	m.textarea.SetValue("")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	um := updated.(*Model)
	if got := um.textarea.Value(); got != "y" {
		t.Fatalf("textarea = %q, want %q (bare y should type, not copy)", got, "y")
	}
}

// copy-last-answer now lives on alt+y (so bare 'y' types normally). alt+y with
// empty input must copy and not insert a character.
func TestAltYCopiesWhenIdleEmpty(t *testing.T) {
	m := NewModel(nil)
	m.history = append(m.history, types.NewAssistantMessage("some answer", nil))
	m.textarea.SetValue("")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}, Alt: true})
	um := updated.(*Model)
	if got := um.textarea.Value(); got != "" {
		t.Fatalf("textarea = %q, want empty (alt+y is a copy command when idle)", got)
	}

	found := false
	for _, n := range um.notices {
		if strings.Contains(n.text, "copied last answer") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected a 'copied last answer' notice after alt+y")
	}
}
