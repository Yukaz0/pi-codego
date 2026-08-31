package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Regression: semua picker (login/logout/model/favorites/thinking/session)
// harus benar-benar menerapkan pilihan saat Enter. Bug lama: Update memakai
// receiver value sehingga mutasi dari closure onConfirm hilang (picker terasa
// "kosong" — balik ke chat tanpa efek).
func TestPickerEnterAppliesSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // never touch the real ~/.pi/agent/auth.json
	m := NewModel(nil)

	// /login -> provider picker
	m.textarea.SetValue("/login")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !m.picker.active || len(m.picker.items) == 0 {
		t.Fatal("picker tidak aktif setelah /login Enter")
	}

	// Enter on a provider -> Pi-style: picker stays open, prompts for API key
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !m.picker.active || !m.picker.prompt || !m.picker.maskInput {
		t.Fatal("setelah pilih provider, harus lanjut prompt API key (masked)")
	}

	// type key -> Enter -> next step asks for base URL
	for _, r := range "sk-test-123" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(*Model)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !m.picker.active || m.picker.maskInput {
		t.Fatal("setelah key, harus lanjut prompt URL (unmasked)")
	}
	if !strings.Contains(m.picker.promptLabel, "base URL") {
		t.Fatalf("prompt label = %q, ingin menanyakan base URL", m.picker.promptLabel)
	}

	// empty URL + Enter -> default endpoint, flow finishes (engine nil in
	// this test, so activation reports an error but picker must close)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if m.picker.active {
		t.Fatal("picker harus tertutup setelah prompt URL selesai")
	}
}
