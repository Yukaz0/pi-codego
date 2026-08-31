package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// /model must be a two-step Pi-style flow: first pick a provider that is
// actually registered (has models in the catalog), then see ONLY that
// provider's models.
func TestModelPickerScopedByProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := map[string]any{
		"openai": map[string]any{"models": []map[string]string{
			{"id": "gpt-4o"}, {"id": "gpt-4o-mini"},
		}},
		"myhost": map[string]any{"models": []map[string]string{
			{"id": "llama-3"},
		}},
	}
	data, _ := json.Marshal(store)
	if err := os.WriteFile(filepath.Join(dir, "models-store.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel(nil)

	// bare /model -> provider picker listing registered providers
	m.textarea.SetValue("/model")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !m.picker.active {
		t.Fatal("picker tidak aktif setelah /model")
	}
	if strings.Contains(strings.Join(m.picker.items, "\n"), "/") {
		t.Fatalf("step 1 harus daftar provider, bukan model: %v", m.picker.items)
	}
	hasMyhost := false
	for _, it := range m.picker.items {
		if it == "myhost" {
			hasMyhost = true
		}
	}
	if !hasMyhost {
		t.Fatalf("provider terdaftar 'myhost' harus muncul: %v", m.picker.items)
	}

	// select myhost -> only myhost models shown
	idx := 0
	for i, it := range m.picker.items {
		if it == "myhost" {
			idx = i
		}
	}
	for m.picker.cursor < idx {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(*Model)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !m.picker.active {
		t.Fatal("step 2 (daftar model) harus terbuka")
	}
	for _, it := range m.picker.items {
		if !strings.HasPrefix(it, "myhost/") {
			t.Fatalf("daftar model harus ter-scope ke myhost, dapat %q", it)
		}
	}

	// /model <provider> shortcut -> scoped picker directly
	m2 := NewModel(nil)
	m2.textarea.SetValue("/model openai")
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 = updated.(*Model)
	if !m2.picker.active {
		t.Fatal("/model openai harus membuka picker model ter-scope")
	}
	for _, it := range m2.picker.items {
		if !strings.HasPrefix(it, "openai/") {
			t.Fatalf("/model openai harus hanya menampilkan model openai, dapat %q", it)
		}
	}
}
