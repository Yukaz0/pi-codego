package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi/pkg/agent"

	tea "github.com/charmbracelet/bubbletea"
)

// newDefaultKeyModel builds a model with a temp HOME containing one custom
// provider ("myhost") that has credentials + a cached model, so the scoped
// /model picker lists exactly "myhost/gpt-4o".
func newDefaultKeyModel(t *testing.T) *Model {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := map[string]any{
		"myhost": map[string]any{"models": []map[string]string{{"id": "gpt-4o"}}},
	}
	data, _ := json.Marshal(store)
	if err := os.WriteFile(filepath.Join(dir, "models-store.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	auth := map[string]any{"myhost": map[string]string{"type": "api-key", "key": "test-key"}}
	data, _ = json.Marshal(auth)
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return NewModel(agent.NewEngine(agent.EngineConfig{}))
}

// Ctrl+D in the model picker selects the highlighted model AND saves it as
// the default in settings.json (same effect as /model <id> --default).
func TestModelPickerCtrlDSavesDefault(t *testing.T) {
	m := newDefaultKeyModel(t)
	m.textarea.SetValue("/model myhost")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !m.picker.active || m.picker.onDefault == nil {
		t.Fatalf("model picker harus aktif dengan hook Ctrl+D, active=%v", m.picker.active)
	}
	if len(m.picker.items) != 1 || m.picker.items[0] != "myhost/gpt-4o" {
		t.Fatalf("items = %v", m.picker.items)
	}

	// hint text must advertise the binding while this picker is open
	m.width, m.height = 100, 30
	if v := m.View(); !strings.Contains(v, "Ctrl+D") {
		t.Error("View harus menampilkan hint Ctrl+D pada model picker")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = updated.(*Model)
	if m.picker.active {
		t.Fatal("picker harus tertutup setelah Ctrl+D")
	}
	if m.engine.Config.Model != "gpt-4o" {
		t.Fatalf("model aktif = %q, ingin gpt-4o", m.engine.Config.Model)
	}
	if m.engine.Config.Provider == nil || m.engine.Config.Provider.Name() != "myhost" {
		t.Fatalf("provider = %v", m.engine.Config.Provider)
	}

	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".pi", "agent", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json tidak tertulis: %v", err)
	}
	var s struct {
		DefaultProvider string `json:"defaultProvider"`
		DefaultModel    string `json:"defaultModel"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.DefaultProvider != "myhost" || s.DefaultModel != "gpt-4o" {
		t.Fatalf("default tersimpan = %+v", s)
	}
	if !strings.Contains(m.viewport.View(), "saved as default") {
		t.Error("transkrip harus mencatat 'saved as default'")
	}
}

// Ctrl+D must be inert in pickers that do not opt in (e.g. the provider
// step of /model), so it can never quit or corrupt those flows.
func TestCtrlDInertInOtherPickers(t *testing.T) {
	m := newDefaultKeyModel(t)
	m.textarea.SetValue("/model")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !m.picker.active {
		t.Fatal("provider picker harus terbuka")
	}
	if m.picker.onDefault != nil {
		t.Fatal("provider picker tidak boleh punya hook Ctrl+D")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = updated.(*Model)
	if !m.picker.active {
		t.Fatal("Ctrl+D harus diabaikan di picker lain")
	}
	m.width, m.height = 100, 30
	if v := m.View(); strings.Contains(v, "Ctrl+D select as default") {
		t.Error("hint Ctrl+D tidak boleh muncul di provider picker")
	}
}
