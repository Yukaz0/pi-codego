package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeStr(m *Model, s string) *Model {
	for _, r := range s {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(*Model)
	}
	return m
}

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
	m = typeStr(m, "sk-test-123")
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

// The [custom] entry inside /login must collect name -> URL -> key and save.
func TestLoginCustomProviderFlow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := NewModel(nil)

	m.textarea.SetValue("/login")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	// move cursor to the [custom] item (last one)
	idx := -1
	for i, it := range m.picker.items {
		if strings.HasPrefix(it, "[custom]") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("picker /login harus punya item [custom] Add a new provider")
	}
	for m.picker.cursor < idx {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(*Model)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !m.picker.active || !m.picker.prompt {
		t.Fatal("[custom] harus membuka prompt chain")
	}
	if !strings.Contains(m.picker.promptLabel, "provider name") {
		t.Fatalf("step 1 = %q, ingin nama provider", m.picker.promptLabel)
	}

	type step struct {
		text  string
		label string // expected next label substring
	}
	for i, s := range []step{
		{"myhost", "base URL"},
		{"not-a-url", "base URL"}, // invalid URL -> re-ask same step
		{"http://127.0.0.1:9/v1", "API key"},
	} {
		m = typeStr(m, s.text)
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(*Model)
		if !m.picker.active {
			t.Fatalf("step %d: picker tertutup terlalu dini", i)
		}
		if !strings.Contains(m.picker.promptLabel, s.label) {
			t.Fatalf("step %d: label = %q, ingin memuat %q", i, m.picker.promptLabel, s.label)
		}
	}
	if !m.picker.maskInput {
		t.Fatal("prompt API key harus masked")
	}

	m = typeStr(m, "sk-custom-abc")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if m.picker.active {
		t.Fatal("picker harus tertutup setelah key")
	}

	// auth.json must contain myhost with baseUrl
	data, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "auth.json"))
	if err != nil {
		t.Fatalf("auth.json tidak ada: %v", err)
	}
	if !strings.Contains(string(data), "myhost") || !strings.Contains(string(data), "127.0.0.1:9") {
		t.Fatalf("auth.json tidak menyimpan custom provider: %s", data)
	}
}
