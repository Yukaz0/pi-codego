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
	m := NewModel(nil)

	// /login -> provider picker
	m.textarea.SetValue("/login")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !m.picker.active || len(m.picker.items) == 0 {
		t.Fatal("picker tidak aktif setelah /login Enter")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if m.picker.active {
		t.Fatal("picker harus tertutup setelah Enter")
	}
	if !strings.HasPrefix(m.textarea.Value(), "/login ") {
		t.Fatalf("textarea = %q, ingin diawali '/login '", m.textarea.Value())
	}
}
