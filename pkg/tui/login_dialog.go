package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"pi/pkg/provider"
)

// builtinProviders are the providers pi-go ships with out of the box.
var builtinProviders = []string{"openai", "anthropic", "gemini", "ollama", "openrouter", "groq", "deepseek", "opencode-go"}

// openProviderPicker shows the built-in provider list (Pi-style selector).
// Selecting a provider immediately prompts for the API key inline.
func (m *Model) openProviderPicker(filter string) {
	providers := make([]string, len(builtinProviders))
	copy(providers, builtinProviders)
	// custom providers previously saved via /login --url
	if saved, err := loadPiAuthMap(); err == nil {
		for p := range saved {
			if !strings.Contains(strings.Join(providers, ","), p) {
				providers = append(providers, p)
			}
		}
	}
	// explicit entry to add a brand-new custom provider from inside /login
	const customItem = "[custom] Add a new provider (URL + API key)..."
	items := m.filterItems(append(providers, customItem), filter)
	m.openPicker("Select provider", items, func(sel string) tea.Cmd {
		if sel == customItem {
			m.promptForCustom()
		} else {
			m.promptForKey(sel)
		}
		return nil
	}, false)
}

// promptForCustom adds a brand-new custom provider entirely from inside
// /login: name -> base URL -> API key -> save + activate.
func (m *Model) promptForCustom() {
	var (
		provName string
		baseURL  string
		step     int
	)
	nameLabel := "Enter provider name (e.g. myhost)"
	m.picker = pickerState{
		active:      true,
		title:       "Login: custom provider",
		prompt:      true,
		promptLabel: nameLabel,
		queryEmpty:  true,
		onPrompt: func(val string) (string, bool) {
			val = strings.TrimSpace(val)
			switch step {
			case 0:
				if val == "" {
					m.appendSystem(errorStyle.Render("Provider name is required"))
					return nameLabel, false
				}
				provName = val
				step = 1
				label := "Enter base URL for " + provName + " (e.g. https://api.myhost.com/v1)"
				m.picker.promptLabel = label
				return label, false
			case 1:
				if !strings.HasPrefix(val, "http://") && !strings.HasPrefix(val, "https://") {
					m.appendSystem(errorStyle.Render("URL must start with http:// or https://"))
					return m.picker.promptLabel, false
				}
				baseURL = val
				step = 2
				m.picker.maskInput = true
				label := "Enter API key for " + provName + " (empty if not required)"
				m.picker.promptLabel = label
				return label, false
			default:
				m.picker.maskInput = false
				m.finishLogin(provName, val, baseURL)
				return "", true
			}
		},
	}
}

// promptForKey starts the Pi-style inline prompt flow for a provider:
// ask API key -> ask custom URL (Enter = default) -> save + activate.
func (m *Model) promptForKey(provider string) {
	var capturedKey string
	m.picker = pickerState{
		active:      true,
		title:       "Login: " + provider,
		prompt:      true,
		maskInput:   true,
		promptLabel: "Enter API key for " + provider + " (Esc to cancel)",
		queryEmpty:  true,
		onPrompt: func(val string) (string, bool) {
			val = strings.TrimSpace(val)
			if m.picker.maskInput {
				// first step: API key
				if val == "" {
					m.appendSystem(errorStyle.Render("API key is required"))
					return "Enter API key for " + provider + " (Esc to cancel)", false
				}
				capturedKey = val
				m.picker.maskInput = false
				return "Enter custom base URL for " + provider + " (empty = default, e.g. https://api.openai.com/v1)", false
			}
			// second step: custom base URL (empty = provider default)
			if val != "" && !strings.HasPrefix(val, "http://") && !strings.HasPrefix(val, "https://") {
				m.appendSystem(errorStyle.Render("URL must start with http:// or https://"))
				return "Enter custom base URL (or leave empty for default)", false
			}
			m.finishLogin(provider, capturedKey, val)
			return "", true
		},
	}
}

// finishLogin saves credentials and activates the provider.
func (m *Model) finishLogin(provName, key, baseURL string) {
	if err := savePiAuth(provName, key, baseURL); err != nil {
		m.appendSystem(errorStyle.Render(fmt.Sprintf("Failed to save: %v", err)))
		return
	}
	msg := "✓ Saved key for " + provName + " to ~/.pi/agent/auth.json"
	if baseURL != "" {
		msg += " (endpoint: " + baseURL + ")"
	}
	m.appendSystem(msg)
	if m.engine == nil {
		return
	}
	if pvd, modelName, err := provider.ResolveProvider(provider.Config{Provider: provName, APIKey: key, BaseURL: baseURL}); err == nil {
		m.engine.Config.Provider = pvd
		if modelName != "" {
			m.engine.Config.Model = modelName
		}
		m.appendSystem(fmt.Sprintf("✓ Provider switched to %s (model: %s)", pvd.Name(), m.engine.Config.Model))
	} else {
		m.appendSystem(errorStyle.Render("Saved, but could not activate now: " + err.Error()))
	}
}

// loadPiAuthMap returns the saved auth.json entries (provider -> record).
func loadPiAuthMap() (map[string]map[string]string, error) {
	dir := piAgentDirTUI()
	if dir == "" {
		return nil, fmt.Errorf("no home dir")
	}
	data, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		return nil, err
	}
	var out map[string]map[string]string
	err = json.Unmarshal(data, &out)
	return out, err
}
