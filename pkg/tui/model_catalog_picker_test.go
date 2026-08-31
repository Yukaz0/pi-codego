package tui

import (
	"strings"
	"testing"
)

// The scoped model picker annotates catalog models with metadata (ctx/cost)
// while leaving custom-provider models without annotation.
func TestModelPickerShowsCatalogMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	m := NewModel(nil)
	m.openModelPickerScoped("anthropic", "")
	found := false
	for _, it := range m.picker.items {
		if note := m.picker.annotations[it]; note != "" && strings.Contains(note, "ctx") {
			found = true
		}
	}
	if !found {
		t.Fatalf("minimal satu model anthropic harus punya anotasi ctx, items=%v notes=%v", m.picker.items, m.picker.annotations)
	}

	// render path must not panic and must include the note text
	rendered := renderPickerAnnotated("Select model", m.picker.items, 0, 120, "", m.picker.annotations)
	if !strings.Contains(rendered, "ctx") {
		t.Fatal("render picker harus menyertakan anotasi metadata")
	}
}
