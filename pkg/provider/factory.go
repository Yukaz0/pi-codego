package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pi/pkg/provider/anthropic"
	"pi/pkg/provider/gemini"
	"pi/pkg/provider/openai"
	"pi/pkg/provider/responses"
	"pi/pkg/types"
)

type Config struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
}

func ResolveProvider(cfg Config) (types.Provider, string, error) {
	// Auto-load dari pi npm config jika flag/env kosong (hindari bentrok tapi reuse key)
	if cfg.Model == "" && cfg.Provider == "" && cfg.APIKey == "" && cfg.BaseURL == "" {
		if s := loadPiSettings(); s != nil {
			if s.DefaultModel != "" && cfg.Model == "" {
				cfg.Model = s.DefaultModel
			}
			if s.DefaultProvider != "" && cfg.Provider == "" {
				cfg.Provider = s.DefaultProvider
			}
		}
	}

	providerName := cfg.Provider
	modelName := cfg.Model
	modelOnOpenRouter := false

	// Allow specifying provider/model in model string, e.g. "openai/gpt-4o", "anthropic/claude-3-5-sonnet"
	if strings.Contains(cfg.Model, "/") && providerName == "" {
		parts := strings.SplitN(cfg.Model, "/", 2)
		providerName = parts[0]
		modelName = parts[1]
	}

	// Provider "stealth" adalah vendor namespace di OpenRouter (model hidden/
	// experimental seperti stealth/ox-alpha). Normalisasi agar PI_MODEL=
	// "stealth/ox-alpha" atau "--provider stealth" tetap resolve via OpenRouter.
	if strings.EqualFold(providerName, "stealth") {
		providerName = "openrouter"
		modelOnOpenRouter = true
	}
	// Model id yang memang di bawah vendor stealth di OpenRouter.
	if strings.HasPrefix(strings.ToLower(modelName), "stealth/") {
		modelOnOpenRouter = true
	}

	if providerName == "" {
		// Cek dulu apakah model ada di opencode-go (muse-spark, kimi, glm, dll) — prioritas
		if api := loadPiModelAPI("opencode-go", modelName); api != "" {
			providerName = "opencode-go"
		} else if strings.HasPrefix(modelName, "claude") {
			providerName = "anthropic"
		} else if strings.HasPrefix(modelName, "gemini") {
			providerName = "gemini"
		} else if strings.HasPrefix(modelName, "llama") || strings.HasPrefix(modelName, "mistral") || strings.HasPrefix(modelName, "deepseek") {
			providerName = "openai" // Ollama / vLLM / OpenRouter compatibility
		} else {
			providerName = "openai"
		}
	}

	// Fallback: jika provider masih opencode-go (dari npm pi), normalisasi
	if providerName == "opencode-go" || providerName == "opencode" {
		// akan ditangani di switch sebagai openai-compatible
	}

	apiKey := cfg.APIKey
	baseURLFromCfg := cfg.BaseURL

	// Helper: coba ambil key dari pi npm auth.json jika masih kosong
	// Strict: hanya exact match, tidak fallback opencode untuk openai/anthropic/gemini
	resolvePiAuthStrict := func(provider string) (string, string) {
		if apiKey != "" {
			return apiKey, baseURLFromCfg
		}
		authMap := loadPiAuth()
		if authMap != nil {
			if rec, ok := authMap[strings.ToLower(provider)]; ok {
				if rec.Key != "" {
					apiKey = rec.Key
				}
				if baseURLFromCfg == "" && rec.BaseURL != "" {
					baseURLFromCfg = rec.BaseURL
				}
			}
		}
		if baseURLFromCfg == "" {
			if u := loadPiBaseURL(provider, modelName); u != "" {
				baseURLFromCfg = u
			}
		}
		return apiKey, baseURLFromCfg
	}
	// Untuk opencode: boleh fallback ke opencode-go
	resolvePiAuthOpencode := func() (string, string) {
		if apiKey != "" && baseURLFromCfg != "" {
			return apiKey, baseURLFromCfg
		}
		authMap := loadPiAuth()
		if authMap != nil && apiKey == "" {
			for _, k := range []string{"opencode-go", "opencode"} {
				if rec, ok := authMap[k]; ok && rec.Key != "" {
					apiKey = rec.Key
					break
				}
			}
		}
		if baseURLFromCfg == "" {
			if u := loadPiBaseURL("opencode-go", modelName); u != "" {
				baseURLFromCfg = u
			}
		}
		return apiKey, baseURLFromCfg
	}

	switch strings.ToLower(providerName) {
	case "anthropic":
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			apiKey, _ = resolvePiAuthStrict(providerName)
		}
		if apiKey == "" {
			return nil, "", fmt.Errorf("ANTHROPIC_API_KEY is not set (dan tidak ada key di ~/.pi/agent/auth.json)")
		}
		return anthropic.NewClient(apiKey, baseURLFromCfg), modelName, nil

	case "gemini", "google":
		if apiKey == "" {
			apiKey = os.Getenv("GEMINI_API_KEY")
		}
		if apiKey == "" {
			apiKey = os.Getenv("GOOGLE_API_KEY")
		}
		if apiKey == "" {
			apiKey, _ = resolvePiAuthStrict(providerName)
		}
		if apiKey == "" {
			return nil, "", fmt.Errorf("GEMINI_API_KEY or GOOGLE_API_KEY is not set (dan tidak ada key di ~/.pi/agent/auth.json)")
		}
		return gemini.NewClient(apiKey, baseURLFromCfg), modelName, nil

	case "opencode", "opencode-go":
		baseURL := baseURLFromCfg
		apiKey, baseURL = resolvePiAuthOpencode()
		if baseURL == "" {
			baseURL = "https://opencode.ai/zen/go/v1"
		}
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" && !strings.Contains(baseURL, "localhost") && !strings.Contains(baseURL, "127.0.0.1") {
			return nil, "", fmt.Errorf("API key for provider %s is not set (set env %s_API_KEY atau isi ~/.pi/agent/auth.json via pi npm)", providerName, strings.ToUpper(providerName))
		}
		// Pilih client berdasarkan api type, wrap agar Name() == opencode-go
		if api := loadPiModelAPI(providerName, modelName); api == "openai-responses" {
			return &opencodeProvider{Provider: responses.NewClient(apiKey, baseURL), name: "opencode-go"}, modelName, nil
		}
		return &opencodeProvider{Provider: openai.NewClient(apiKey, baseURL), name: "opencode-go"}, modelName, nil

	case "openai", "openrouter", "groq", "ollama", "deepseek":
		baseURL := baseURLFromCfg
		// strict: jangan fallback ke opencode untuk provider eksplisit
		apiK, baseU := resolvePiAuthStrict(providerName)
		if apiKey == "" {
			apiKey = apiK
		}
		if baseURL == "" {
			baseURL = baseU
		}
		// stealth/* hanya ada di OpenRouter: paksa rute + key OpenRouter
		// jika belum ada credential spesifik.
		if modelOnOpenRouter {
			// SplitN("stealth/ox-alpha") sudah memakan vendor prefix;
			// kembalikan agar model id sesuai katalog OpenRouter.
			if !strings.Contains(modelName, "/") {
				modelName = "stealth/" + modelName
			}
			if apiKey == "" {
				if rec := loadPiAuth(); rec != nil {
					if e, ok := rec["openrouter"]; ok && e.Key != "" {
						apiKey = e.Key
					}
				}
			}
			if apiKey == "" {
				apiKey = os.Getenv("OPENROUTER_API_KEY")
			}
			// Paksa rute OpenRouter kecuali user set --base-url eksplisit.
			if cfg.BaseURL == "" {
				baseURL = "https://openrouter.ai/api/v1"
			}
		}
		if baseURL == "" {
			switch strings.ToLower(providerName) {
			case "openrouter":
				baseURL = "https://openrouter.ai/api/v1"
				if apiKey == "" {
					apiKey = os.Getenv("OPENROUTER_API_KEY")
				}
			case "groq":
				baseURL = "https://api.groq.com/openai/v1"
				if apiKey == "" {
					apiKey = os.Getenv("GROQ_API_KEY")
				}
			case "ollama":
				baseURL = "http://localhost:11434/v1"
				if apiKey == "" {
					apiKey = "ollama"
				}
			case "deepseek":
				baseURL = "https://api.deepseek.com/v1"
				if apiKey == "" {
					apiKey = os.Getenv("DEEPSEEK_API_KEY")
				}
			case "opencode", "opencode-go":
				if baseURL == "" {
					baseURL = "https://opencode.ai/zen/go/v1"
				}
				if apiKey == "" {
					apiKey = os.Getenv("OPENAI_API_KEY")
				}
			default:
				if apiKey == "" {
					apiKey = os.Getenv("OPENAI_API_KEY")
				}
			}
		}
		if apiKey == "" && !strings.Contains(baseURL, "localhost") && !strings.Contains(baseURL, "127.0.0.1") {
			return nil, "", fmt.Errorf("API key for provider %s is not set (set env %s_API_KEY)", providerName, strings.ToUpper(providerName))
		}
		return openai.NewClient(apiKey, baseURL), modelName, nil

	default:
		// coba fallback ke pi npm auth: jika provider tidak dikenal tapi ada di auth.json, treat sebagai openai-compatible
		if m := loadPiAuth(); m != nil {
			if rec, ok := m[strings.ToLower(providerName)]; ok && rec.Key != "" {
				apiKey = rec.Key
				baseURL := baseURLFromCfg
				if baseURL == "" {
					baseURL = rec.BaseURL
				}
				if baseURL == "" {
					baseURL = loadPiBaseURL(providerName, modelName)
					if baseURL == "" {
						baseURL = "https://opencode.ai/zen/go/v1"
					}
				}
				// Wrap agar Name() == provider asli (mis. custom)
				return &opencodeProvider{Provider: openai.NewClient(apiKey, baseURL), name: strings.ToLower(providerName)}, modelName, nil
			}
		}
		return nil, "", fmt.Errorf("unsupported provider: %s", providerName)
	}
}

// --- pi npm interop: baca ~/.pi/agent/auth.json & settings.json & models-store.json ---

type piAuthEntry struct {
	Type    string `json:"type"`
	Key     string `json:"key"`
	BaseURL string `json:"baseUrl,omitempty"`
}

type piSettings struct {
	DefaultProvider string `json:"defaultProvider"`
	DefaultModel    string `json:"defaultModel"`
}

func piAgentDir() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".pi", "agent")
}

func loadPiAuth() map[string]piAuthEntry {
	dir := piAgentDir()
	if dir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		return nil
	}
	var m map[string]piAuthEntry
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	// normalisasi key lowercase
	norm := make(map[string]piAuthEntry, len(m))
	for k, v := range m {
		norm[strings.ToLower(k)] = v
	}
	return norm
}

func loadPiSettings() *piSettings {
	dir := piAgentDir()
	if dir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return nil
	}
	var s piSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return &s
}

func loadPiModelAPI(provider, model string) string {
	dir := piAgentDir()
	if dir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "models-store.json"))
	if err != nil {
		return ""
	}
	var raw map[string]struct {
		Models []struct {
			ID  string `json:"id"`
			API string `json:"api"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return ""
	}
	provLower := strings.ToLower(provider)
	if entry, ok := raw[provLower]; ok {
		for _, m := range entry.Models {
			if m.ID == model {
				return m.API
			}
		}
	}
	// fallback opencode
	for _, key := range []string{"opencode-go", "opencode"} {
		if entry, ok := raw[key]; ok {
			for _, m := range entry.Models {
				if m.ID == model {
					return m.API
				}
			}
		}
	}
	return ""
}

// opencodeProvider wraps openai/responses client to report correct provider name
type opencodeProvider struct {
	types.Provider
	name string
}

func (o *opencodeProvider) Name() string { return o.name }

func loadPiBaseURL(provider, model string) string {
	dir := piAgentDir()
	if dir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "models-store.json"))
	if err != nil {
		return ""
	}
	var raw map[string]struct {
		Models []struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
			BaseURL  string `json:"baseUrl"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return ""
	}
	provLower := strings.ToLower(provider)
	// cari provider exact
	if entry, ok := raw[provLower]; ok && len(entry.Models) > 0 {
		// coba match model id dulu
		for _, m := range entry.Models {
			if m.ID == model && m.BaseURL != "" {
				return m.BaseURL
			}
		}
		if entry.Models[0].BaseURL != "" {
			return entry.Models[0].BaseURL
		}
	}
	// fallback: cari di semua provider dengan key opencode-go
	for _, key := range []string{"opencode-go", "opencode"} {
		if entry, ok := raw[key]; ok && len(entry.Models) > 0 && entry.Models[0].BaseURL != "" {
			return entry.Models[0].BaseURL
		}
	}
	return ""
}
