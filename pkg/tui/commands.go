package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	piContext "pi/pkg/context"
	"pi/pkg/provider"
	"pi/pkg/provider/modelcatalog"
	"pi/pkg/session"
	"pi/pkg/update"
)

// slashCommand defines a slash command
// slashSuggestions returns commands (and prompt templates) whose name starts
// with the typed prefix. prefix must begin with "/" and contain no space.
// Results are sorted by name; aliases are included.
func (m *Model) slashSuggestions(prefix string) []slashCommand {
	if !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, " \n") {
		return nil
	}
	q := strings.ToLower(strings.TrimPrefix(prefix, "/"))
	seen := map[string]bool{}
	var out []slashCommand
	names := make([]string, 0, len(slashCommands))
	for n := range slashCommands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if strings.HasPrefix(n, q) {
			out = append(out, slashCommands[n])
			seen[n] = true
		}
	}
	// prompt templates from .pi/prompts
	if m.engine != nil && m.engine.Config.PromptLoader != nil {
		var tpls []slashCommand
		for _, tpl := range m.engine.Config.PromptLoader.LoadAll() {
			if seen[tpl.Name] || !strings.HasPrefix(strings.ToLower(tpl.Name), q) {
				continue
			}
			desc := tpl.Description
			if desc == "" {
				desc = "prompt template"
			}
			tpls = append(tpls, slashCommand{Name: tpl.Name, Description: desc, Usage: "/" + tpl.Name})
		}
		sort.Slice(tpls, func(i, j int) bool { return tpls[i].Name < tpls[j].Name })
		out = append(out, tpls...)
	}
	// skills exposed as /skill:<name> (Pi registers them as slash commands)
	for _, sk := range m.loadedSkills() {
		name := "skill:" + sk.Name
		if seen[name] {
			continue
		}
		seen[name] = true
		if !strings.HasPrefix(strings.ToLower(name), q) {
			continue
		}
		desc := sk.Description
		if desc == "" {
			desc = "agent skill"
		}
		out = append(out, slashCommand{Name: name, Description: desc, Usage: "/" + name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// loadedSkills returns the skills visible to the engine (nil-safe).
func (m *Model) loadedSkills() []piContext.Skill {
	if m.engine == nil || m.engine.Config.SkillLoader == nil {
		return nil
	}
	skills, _ := m.engine.Config.SkillLoader.LoadAvailableSkills()
	return skills
}

// promptTemplates returns the loaded .pi/prompts templates (nil-safe).
func (m *Model) promptTemplates() []piContext.PromptTemplate {
	if m.engine == nil || m.engine.Config.PromptLoader == nil {
		return nil
	}
	return m.engine.Config.PromptLoader.LoadAll()
}

// lookupSkill finds a loaded skill by name (case-insensitive).
func lookupSkill(m *Model, name string) (piContext.Skill, bool) {
	for _, sk := range m.loadedSkills() {
		if strings.EqualFold(sk.Name, name) {
			return sk, true
		}
	}
	return piContext.Skill{}, false
}

// completeSlash inserts the highlighted suggestion into the textarea.
func (m *Model) completeSlash() {
	sugs := m.slashSuggestions(m.textarea.Value())
	if len(sugs) == 0 {
		return
	}
	idx := m.slashCursor % len(sugs)
	m.textarea.SetValue("/" + sugs[idx].Name + " ")
	m.slashCursor = 0
}

type slashCommand struct {
	Name        string
	Description string
	Usage       string
	Handler     func(m *Model, args string) tea.Cmd
}

var slashCommands map[string]slashCommand

func init() {
	slashCommands = map[string]slashCommand{
		"help":          {Name: "help", Description: "Show available commands", Usage: "/help", Handler: handleHelp},
		"hotkeys":       {Name: "hotkeys", Description: "Show keyboard shortcuts", Usage: "/hotkeys", Handler: handleHotkeys},
		"changelog":     {Name: "changelog", Description: "Show version history", Usage: "/changelog", Handler: handleChangelog},
		"update":        {Name: "update", Description: "Check for a newer release and update the binary now", Usage: "/update", Handler: handleUpdate},
		"model":         {Name: "model", Description: "Switch models: /model [provider/model] or /model", Usage: "/model [provider/model]", Handler: handleModel},
		"login":         {Name: "login", Description: "Configure provider authentication", Usage: "/login [provider] [api-key] [--url <endpoint>] [--model <id>]", Handler: handleLogin},
		"logout":        {Name: "logout", Description: "Remove provider credentials: /logout [provider]", Usage: "/logout [provider]", Handler: handleLogout},
		"new":           {Name: "new", Description: "Start a new session", Usage: "/new", Handler: handleNew},
		"clear":         {Name: "clear", Description: "Clear current session (alias /new)", Usage: "/clear", Handler: handleNew},
		"session":       {Name: "session", Description: "Show session info", Usage: "/session", Handler: handleSessionInfo},
		"tree":          {Name: "tree", Description: "Show session tree / branches", Usage: "/tree", Handler: handleTree},
		"compact":       {Name: "compact", Description: "Manually compact context", Usage: "/compact [instructions]", Handler: handleCompact},
		"export":        {Name: "export", Description: "Export session to file: /export [file]", Usage: "/export [file]", Handler: handleExport},
		"copy":          {Name: "copy", Description: "Show last assistant message", Usage: "/copy", Handler: handleCopy},
		"reload":        {Name: "reload", Description: "Reload context files (AGENTS.md, skills)", Usage: "/reload", Handler: handleReload},
		"quit":          {Name: "quit", Description: "Quit pi", Usage: "/quit", Handler: handleQuit},
		"q":             {Name: "q", Description: "Quit pi (alias)", Usage: "/q", Handler: handleQuit},
		"name":          {Name: "name", Description: "Set session display name: /name <name>", Usage: "/name <name>", Handler: handleName},
		"resume":        {Name: "resume", Description: "List/resume previous sessions", Usage: "/resume [id]", Handler: handleResume},
		"settings":      {Name: "settings", Description: "Show current settings & model", Usage: "/settings", Handler: handleSettings},
		"scoped-models": {Name: "scoped-models", Description: "List available models for provider", Usage: "/scoped-models", Handler: handleScopedModels},
		"mcp":           {Name: "mcp", Description: "MCP servers: /mcp list|reload|status", Usage: "/mcp [list|reload|status]", Handler: handleMCP},
		"favorite":      {Name: "favorite", Description: "Manage favorites: /favorite add|remove|list [model]", Usage: "/favorite add <model> | /favorite remove <model> | /favorite list", Handler: handleFavorite},
		"favorites":     {Name: "favorites", Description: "List favorite models (alias)", Usage: "/favorites", Handler: handleFavoriteList},
		"fav":           {Name: "fav", Description: "Alias for /favorite", Usage: "/fav [add|remove|list]", Handler: handleFavorite},
		"trust":         {Name: "trust", Description: "Show project trust status (AGENTS.md loaded)", Usage: "/trust", Handler: handleTrust},
		"fork":          {Name: "fork", Description: "Branch conversation from an earlier message", Usage: "/fork [msg#]", Handler: handleFork},
		"clone":         {Name: "clone", Description: "Duplicate current session", Usage: "/clone", Handler: handleClone},
		"share":         {Name: "share", Description: "Not implemented: share via gist", Usage: "/share", Handler: handleNotImpl},
		"import":        {Name: "import", Description: "Import session: /import <file>", Usage: "/import <file>", Handler: handleImport},
		"think":         {Name: "think", Description: "Set thinking effort: /think <off|minimal|low|medium|high|xhigh|max>", Usage: "/think <level>", Handler: handleThink},
		"bg":            {Name: "bg", Description: "Run a background task: /bg <command> or /bg list|cancel <id>", Usage: "/bg <command> | /bg list | /bg cancel <id>", Handler: handleBg},
		"tools":         {Name: "tools", Description: "List available tools", Usage: "/tools", Handler: handleTools},
		"status":        {Name: "status", Description: "Show current TUI status (model, branch, tokens, bg)", Usage: "/status", Handler: handleStatus},
		"cd":            {Name: "cd", Description: "Change working directory: /cd <path>", Usage: "/cd <path>", Handler: handleCd},
		"llama":         {Name: "llama", Description: "Not implemented: llama.cpp manager", Usage: "/llama", Handler: handleNotImpl},
	}
}

// handleSlash checks if input is slash command and executes it
// returns (handled bool, cmd). expandPrompt is non-empty when a prompt
// template matched: the caller should submit it to the agent as a turn.
func handleSlash(m *Model, input string) (handled bool, cmd tea.Cmd, expandPrompt string) {
	if !strings.HasPrefix(input, "/") {
		return false, nil, ""
	}
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return false, nil, ""
	}
	cmdName := strings.TrimPrefix(parts[0], "/")
	cmdName = strings.ToLower(cmdName)
	args := ""
	if len(parts) > 1 {
		args = strings.Join(parts[1:], " ")
	}
	// support /skill:name expansion — Pi injects the skill body as the user
	// message wrapped in a <skill> block, args appended after it.
	if strings.HasPrefix(cmdName, "skill:") {
		skillName := strings.TrimPrefix(cmdName, "skill:")
		if sk, ok := lookupSkill(m, skillName); ok {
			block := piContext.FormatSkillBlock(sk)
			if args != "" {
				block += "\n\n" + args
			}
			return true, nil, block
		}
		m.appendSystem(fmt.Sprintf("Skill '%s' not found — run /reload after adding SKILL.md, or check /help for loaded skills", skillName))
		return true, nil, ""
	}
	if cmd, ok := slashCommands[cmdName]; ok {
		return true, cmd.Handler(m, args), ""
	}
	// prompt template expansion: /templatename [args]
	if m.engine != nil && m.engine.Config.PromptLoader != nil {
		if tpl, ok := m.engine.Config.PromptLoader.Get(cmdName); ok {
			return true, nil, tpl.Expand(args)
		}
	}
	// semua metode pilih pakai arrow+Enter — fallback ke command picker filtered
	m.openCommandPicker(cmdName)
	return true, nil, ""
}

// expandSlashForSteer applies Pi's mid-stream expansion: /skill:<name> and
// prompt templates expand into their text before being queued as a steer
// message; anything else (including built-in commands) passes through verbatim.
func expandSlashForSteer(m *Model, input string) string {
	if !strings.HasPrefix(input, "/") {
		return input
	}
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return input
	}
	cmdName := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	args := ""
	if len(parts) > 1 {
		args = strings.Join(parts[1:], " ")
	}
	if strings.HasPrefix(cmdName, "skill:") {
		if sk, ok := lookupSkill(m, strings.TrimPrefix(cmdName, "skill:")); ok {
			block := piContext.FormatSkillBlock(sk)
			if args != "" {
				block += "\n\n" + args
			}
			return block
		}
		return input
	}
	if _, isBuiltin := slashCommands[cmdName]; isBuiltin {
		return input
	}
	if m.engine != nil && m.engine.Config.PromptLoader != nil {
		if tpl, ok := m.engine.Config.PromptLoader.Get(cmdName); ok {
			return tpl.Expand(args)
		}
	}
	return input
}

// --- handlers ---

func handleHelp(m *Model, _ string) tea.Cmd {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render(" Available Slash Commands ") + "\n\n")
	// group
	groups := []struct {
		title string
		cmds  []string
	}{
		{"Auth & Model", []string{"login", "logout", "model", "favorite", "favorites", "scoped-models"}},
		{"Session", []string{"new", "session", "tree", "compact", "export", "import", "resume", "name", "fork", "clone"}},
		{"System", []string{"mcp", "settings", "reload", "trust", "copy", "share", "hotkeys", "changelog", "update", "quit"}},
		{"Modes", []string{"think", "bg", "tools", "status", "cd"}},
	}
	for _, g := range groups {
		sb.WriteString(statusStyle.Render(g.title+":") + "\n")
		for _, n := range g.cmds {
			if c, ok := slashCommands[n]; ok {
				sb.WriteString(fmt.Sprintf("  %-15s %s\n", "/"+c.Name, c.Description))
			}
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	// prompt templates (.pi/prompts/*.md) and skills, so they are discoverable
	if tpls := m.promptTemplates(); len(tpls) > 0 {
		sb.WriteString(statusStyle.Render("Prompt templates (.pi/prompts, ~/.pi/agent/prompts):") + "\n")
		for _, t := range tpls {
			sb.WriteString(fmt.Sprintf("  %-15s %s\n", "/"+t.Name, t.Description))
		}
		sb.WriteString("\n")
	}
	if skills := m.loadedSkills(); len(skills) > 0 {
		sb.WriteString(statusStyle.Render("Agent skills:") + "\n")
		for _, sk := range skills {
			sb.WriteString(fmt.Sprintf("  %-15s %s\n", "/skill:"+sk.Name, sk.Description))
		}
		sb.WriteString("\n")
	}
	sb.WriteString(helpStyle.Render("Tip: Type /model openai/gpt-4o to switch, /login opencode-go <key> to save key") + "\n")
	sb.WriteString(helpStyle.Render("Outside the TUI: pi-go update [--check] · pi-go sessions · pi-go config · pi-go doctor"))
	m.appendRendered(sb.String())
	m.viewport.GotoBottom()
	return nil
}

func handleHotkeys(m *Model, _ string) tea.Cmd {
	help := `
` + headerStyle.Render(" Hotkeys ") + `
` + helpStyle.Render("enter: send · enter (streaming): steer") + `
` + helpStyle.Render("ctrl+c: interrupt / quit · ctrl+l: clear · ?: help") + `
` + helpStyle.Render("pgup/pgdown: scroll · ctrl+u/ctrl+d: scroll") + `
` + helpStyle.Render("y: copy last answer (plain text, selectable)") + `
` + helpStyle.Render("ctrl+p: command palette (↑/↓ + Enter) · ctrl+o: model picker") + `
` + helpStyle.Render("model picker: Ctrl+D = select and save as default model") + `
` + helpStyle.Render("semua metode pilih: ↑/↓ navigate · Enter confirm · Esc cancel") + `
` + helpStyle.Render("shift+enter: multi-line (if terminal supports)") + `
` + helpStyle.Render("Commands: /help for slash commands") + `
`
	m.appendRendered(help)
	m.viewport.GotoBottom()
	return nil
}

// updateCmd checks GitHub Releases right now and, when a newer version is
// published, downloads and atomically replaces the on-disk binary. The
// running session keeps the old version until restarted.
func updateCmd(cur string) tea.Msg {
	to, err := update.Update(cur)
	return updateResultMsg{From: cur, To: to, Err: err}
}

func handleUpdate(m *Model, _ string) tea.Cmd {
	m.appendSystem(statusStyle.Render(fmt.Sprintf("checking for updates (current: %s)…", appVersion)))
	m.viewport.GotoBottom()
	return func() tea.Msg { return updateCmd(appVersion) }
}

func handleChangelog(m *Model, _ string) tea.Cmd {
	msg := headerStyle.Render(" pi-code-golang Changelog ") + "\n\n" +
		statusStyle.Render("v0.1.0 — Initial Go port\n") +
		"  • Multi-provider streaming (OpenAI/Anthropic/Gemini/Ollama)\n" +
		"  • Tree session + JSONL + compaction\n" +
		"  • TUI Bubbletea + RPC + print mode\n" +
		"  • Slash commands (/model, /login, /help, ...)\n" +
		"  • Auto-read pi npm auth.json (opencode-go)\n"
	m.appendRendered(msg)
	m.viewport.GotoBottom()
	return nil
}

func handleModel(m *Model, args string) tea.Cmd {
	if strings.TrimSpace(args) == "" {
		// Pi-style two-step: pick a registered provider first, then only
		// that provider's models are listed.
		m.openModelProviderPicker()
		return nil
	}
	// handle --default flag
	argsTrim := strings.TrimSpace(args)
	makeDefault := false
	if strings.HasSuffix(argsTrim, "--default") || strings.HasSuffix(argsTrim, "--save") {
		makeDefault = true
		argsTrim = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(argsTrim, "--default"), "--save"))
	}
	// /model <provider> (no slash in arg, matches a catalog provider) ->
	// scoped model list for that provider only.
	if !strings.Contains(argsTrim, "/") && !strings.Contains(argsTrim, " ") {
		provs := map[string]bool{}
		for _, full := range m.availableModels() {
			if i := strings.Index(full, "/"); i > 0 {
				provs[strings.ToLower(full[:i])] = true
			}
		}
		if provs[strings.ToLower(argsTrim)] {
			m.openModelPickerScoped(argsTrim, "")
			return nil
		}
	}
	modelArg := strings.ReplaceAll(argsTrim, " ", "/")
	pvd, modelName, err := provider.ResolveProvider(provider.Config{Model: modelArg})
	if err != nil {
		// fallback: open picker filtered by query — arrow + Enter to select (per request)
		m.openModelPicker(argsTrim)
		return nil
	}
	m.engine.Config.Provider = pvd
	m.engine.Config.Model = modelName
	// keep the compaction budget in sync with the new model's context window
	if c := m.engine.Config.Compactor; c != nil {
		if info, ok := modelcatalog.Lookup(pvd.Name(), modelName); ok && info.ContextWindow > 0 {
			c.MaxTokens = info.ContextWindow * 3 / 4
		} else {
			c.MaxTokens = 0 // unknown model: message-count heuristic only
		}
	}
	msg := fmt.Sprintf("✓ Switched to %s (%s) — %s", modelName, pvd.Name(), modelArg)
	if makeDefault {
		if err := saveDefaultModel(pvd.Name(), modelName); err == nil {
			msg += " ★ saved as default (settings.json)"
		} else {
			msg += fmt.Sprintf(" (save default failed: %v)", err)
		}
	}
	m.appendSystem(msg)
	m.refreshViewport()
	return nil
}

// parseLoginArgs splits "/login [provider] [key] [--url <endpoint>] [--model <id>]".
// Unknown provider names are accepted as custom OpenAI-compatible endpoints
// (requires --url). Returns provider="" when only flags were given.
func parseLoginArgs(args string) (providerName, apiKey, baseURL, model string, err error) {
	parts := strings.Fields(args)
	var positional []string
	for i := 0; i < len(parts); i++ {
		switch strings.ToLower(parts[i]) {
		case "--url", "-u":
			if i+1 >= len(parts) {
				return "", "", "", "", fmt.Errorf("--url requires a value")
			}
			i++
			baseURL = parts[i]
		case "--key", "-k":
			if i+1 >= len(parts) {
				return "", "", "", "", fmt.Errorf("--key requires a value")
			}
			i++
			apiKey = parts[i]
		case "--model", "-m":
			if i+1 >= len(parts) {
				return "", "", "", "", fmt.Errorf("--model requires a value")
			}
			i++
			model = parts[i]
		default:
			positional = append(positional, parts[i])
		}
	}
	if len(positional) > 0 {
		providerName = strings.ToLower(positional[0])
	}
	if apiKey == "" && len(positional) > 1 {
		apiKey = strings.Join(positional[1:], " ")
	}
	if baseURL != "" && !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return "", "", "", "", fmt.Errorf("--url must start with http:// or https://")
	}
	return providerName, apiKey, baseURL, model, nil
}

func handleLogin(m *Model, args string) tea.Cmd {
	providerName, apiKey, baseURL, model, perr := parseLoginArgs(args)
	if perr != nil {
		m.appendSystem(errorStyle.Render("/login: " + perr.Error()))
		return nil
	}
	known := map[string]bool{"openai": true, "anthropic": true, "gemini": true, "ollama": true, "openrouter": true, "groq": true, "deepseek": true, "opencode-go": true}
	// Pi-style: bare /login opens the provider selector, then prompts inline
	// for the API key and (optionally) a custom base URL.
	if providerName == "" {
		m.openProviderPicker("")
		return nil
	}
	// /login <provider> — jump straight into the key/URL prompt for it.
	if apiKey == "" && baseURL == "" && model == "" {
		m.promptForKey(providerName)
		return nil
	}
	// Explicit form (custom provider shortcut): /login <provider> <key> [--url ...] [--model ...]
	if !known[providerName] && baseURL == "" {
		m.appendSystem(helpStyle.Render(fmt.Sprintf(
			"Provider '%s' is not built-in — a custom endpoint is required:\\n"+
				"  /login %s <api-key> --url https://my-server.example.com/v1 [--model some-model]",
			providerName, providerName)))
		return nil
	}
	if apiKey == "" && baseURL != "" && !strings.Contains(baseURL, "localhost") && !strings.Contains(baseURL, "127.0.0.1") {
		m.appendSystem(errorStyle.Render("Missing api-key. Usage: /login " + providerName + " <api-key> --url <endpoint>"))
		return nil
	}
	if err := savePiAuth(providerName, apiKey, baseURL); err != nil {
		m.appendSystem(errorStyle.Render(fmt.Sprintf("Failed to save: %v", err)))
		return nil
	}
	msg := fmt.Sprintf("✓ Saved key for %s to ~/.pi/agent/auth.json", providerName)
	if baseURL != "" {
		msg += " (endpoint: " + baseURL + ")"
	}
	m.appendSystem(msg)
	if pvd, modelName, err := provider.ResolveProvider(provider.Config{Provider: providerName, APIKey: apiKey, BaseURL: baseURL, Model: model}); err == nil {
		m.engine.Config.Provider = pvd
		if model != "" {
			m.engine.Config.Model = model
		} else if modelName != "" {
			m.engine.Config.Model = modelName
		}
		m.appendSystem(fmt.Sprintf("✓ Provider switched to %s (model: %s)", pvd.Name(), m.engine.Config.Model))
	} else {
		m.appendSystem(errorStyle.Render("Saved, but could not activate now: " + err.Error()))
	}
	return nil
}

func handleLogout(m *Model, args string) tea.Cmd {
	providerName := strings.TrimSpace(args)
	if providerName == "" {
		// Pi-style: /logout only lists providers with stored credentials.
		var items []string
		if saved, err := loadPiAuthMap(); err == nil {
			for p := range saved {
				items = append(items, p)
			}
		}
		if len(items) == 0 {
			m.appendSystem("No stored credentials to remove. /logout only removes credentials saved by /login; environment variables are unchanged.")
			return nil
		}
		sort.Strings(items)
		m.openPicker("Remove credentials for", items, func(sel string) tea.Cmd {
			if err := removePiAuth(sel); err != nil {
				m.appendSystem(errorStyle.Render(fmt.Sprintf("Failed to remove: %v", err)))
			} else {
				m.appendSystem("✓ Removed stored credentials for " + sel + ". Environment variables are unchanged.")
			}
			return nil
		}, false)
		return nil
	}
	providerName = strings.ToLower(providerName)
	if err := removePiAuth(providerName); err != nil {
		m.appendRendered(errorStyle.Render(fmt.Sprintf("Failed to remove: %v", err)))
		m.viewport.GotoBottom()
		return nil
	}
	m.appendSystem(fmt.Sprintf("✓ Removed key for %s from ~/.pi/agent/auth.json", providerName))
	return nil
}

func handleTree(m *Model, _ string) tea.Cmd {
	tree := m.engine.Config.SessionTree
	if tree == nil || len(tree.Nodes) == 0 {
		m.appendRendered(statusStyle.Render("Tree is empty — no messages yet"))
		m.viewport.GotoBottom()
		return nil
	}
	hist := tree.GetLinearHistory("")
	var sb strings.Builder
	sb.WriteString(headerStyle.Render(" Session Tree ") + "\n\n")
	for i, msg := range hist {
		marker := "○"
		if i == len(hist)-1 {
			marker = "● current"
		}
		roleColor := statusStyle
		if msg.Role == "user" {
			roleColor = helpStyle
		}
		contentPreview := strings.ReplaceAll(msg.Content, "\n", " ")
		if len(contentPreview) > 60 {
			contentPreview = contentPreview[:60] + "…"
		}
		sb.WriteString(fmt.Sprintf("%s %s [%s] %s\n", marker, roleColor.Render(string(msg.Role)), msg.ID[:min(6, len(msg.ID))], contentPreview))
	}
	sb.WriteString("\n" + helpStyle.Render("Tree is linear in Go port. Branching via SwitchBranch is available via API."))
	m.appendRendered(sb.String())
	m.viewport.GotoBottom()
	return nil
}

func handleCompact(m *Model, args string) tea.Cmd {
	tree := m.engine.Config.SessionTree
	if tree == nil || len(tree.Nodes) == 0 {
		m.appendRendered(errorStyle.Render("No session to compact"))
		m.viewport.GotoBottom()
		return nil
	}
	hist := tree.GetLinearHistory("")
	if len(hist) <= 4 {
		m.appendRendered(statusStyle.Render(fmt.Sprintf("Nothing to compact: %d messages", len(hist))))
		m.viewport.GotoBottom()
		return nil
	}
	compactor := m.engine.Config.Compactor
	if compactor == nil {
		m.appendSystem("No compactor configured on this engine")
		return nil
	}
	instr := strings.TrimSpace(args)
	m.appendSystem(fmt.Sprintf("Compacting %d messages…", len(hist)))
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		// optional user instructions bias the summary
		if instr != "" && compactor.Provider != nil {
			compactor.ExtraInstructions = instr
		}
		compacted, changed, err := compactor.Compact(ctx, hist)
		if err != nil {
			return statusMsg("Compact failed: " + err.Error())
		}
		if !changed {
			return statusMsg("Nothing to compact")
		}
		newTree := session.NewTree()
		newTree.SetName(tree.GetName())
		for _, msg := range compacted {
			newTree.AddMessage(msg)
		}
		m.engine.Config.SessionTree = newTree
		m.history = compacted
		m.invalidateChatCache()
		m.autoSaveSession()
		return statusMsg(fmt.Sprintf("✓ Compacted %d → %d messages", len(hist), len(compacted)))
	}
}

func handleExport(m *Model, args string) tea.Cmd {
	path := strings.TrimSpace(args)
	if path == "" {
		path = "pi-session-export.jsonl"
	}
	tree := m.engine.Config.SessionTree
	if tree == nil {
		m.appendRendered(errorStyle.Render("No session to export"))
		m.viewport.GotoBottom()
		return nil
	}
	// save via session.Storage
	// minimal: write history as JSONL
	f, err := os.Create(path)
	if err != nil {
		m.appendRendered(errorStyle.Render(fmt.Sprintf("Export failed: %v", err)))
		m.viewport.GotoBottom()
		return nil
	}
	defer f.Close()
	hist := tree.GetLinearHistory("")
	for _, msg := range hist {
		b, _ := json.Marshal(msg)
		fmt.Fprintln(f, string(b))
	}
	m.appendSystem(fmt.Sprintf("✓ Exported %d messages to %s", len(hist), path))
	return nil
}

func handleCopy(m *Model, _ string) tea.Cmd {
	hist := m.engine.Config.SessionTree.GetLinearHistory("")
	if len(hist) == 0 {
		m.appendRendered(statusStyle.Render("No messages to copy"))
		m.viewport.GotoBottom()
		return nil
	}
	// find last assistant
	for i := len(hist) - 1; i >= 0; i-- {
		if hist[i].Role == "assistant" && hist[i].Content != "" {
			m.appendRendered(headerStyle.Render(" Last Assistant Message ") + "\n\n" + renderMarkdown(hist[i].Content))
			m.viewport.GotoBottom()
			return nil
		}
	}
	m.appendRendered(statusStyle.Render("No assistant message found"))
	m.viewport.GotoBottom()
	return nil
}

func handleReload(m *Model, _ string) tea.Cmd {
	m.appendSystem("Reloading AGENTS.md, SKILL.md, keybindings...")
	// In Go port, context is loaded per Turn, so just notify
	m.appendRendered(statusStyle.Render("✓ Reloaded — next turn will re-read AGENTS.md & SKILL.md"))
	m.viewport.GotoBottom()
	return nil
}

func handleQuit(m *Model, _ string) tea.Cmd {
	return tea.Quit
}

func handleSettings(m *Model, _ string) tea.Cmd {
	provName := "none"
	if m.engine.Config.Provider != nil {
		provName = m.engine.Config.Provider.Name()
	}
	info := fmt.Sprintf("%s\nProvider: %s\nModel: %s\nWorkdir: %s\nSessions: ~/.pi/agent/sessions/\nAuth: ~/.pi/agent/auth.json\n",
		headerStyle.Render(" Settings "),
		provName,
		m.engine.Config.Model,
		func() string { wd, _ := os.Getwd(); return wd }(),
	)
	m.appendRendered(info)
	m.viewport.GotoBottom()
	return nil
}

func handleScopedModels(m *Model, _ string) tea.Cmd {
	return handleModel(m, "")
}

func handleMCP(m *Model, args string) tea.Cmd {
	mgr := GetMCPManager()
	cmd := strings.TrimSpace(strings.ToLower(args))
	if cmd == "" || cmd == "list" || cmd == "status" {
		if mgr == nil || len(mgr.Clients) == 0 {
			var cfgPath string
			for _, p := range []string{filepath.Join(piAgentDirTUI(), "mcp.json"), ".pi/mcp.json", ".mcp.json"} {
				if _, err := os.Stat(p); err == nil {
					cfgPath = p
					break
				}
			}
			msg := headerStyle.Render(" MCP ") + "\n\n" + statusStyle.Render("No MCP servers connected\n")
			if cfgPath == "" {
				msg += helpStyle.Render("No config found. Create ~/.pi/agent/mcp.json:\n")
				msg += helpStyle.Render(`{
  "mcpServers": {
    "filesystem": {"command":"npx","args":["-y","@modelcontextprotocol/server-filesystem","/home/neu/Documents"]},
    "codebase-memory": {"command":"npx","args":["-y","codebase-memory-mcp"]}
  }
}`) + "\n"
			} else {
				msg += helpStyle.Render(fmt.Sprintf("Config: %s — check server command & npx installed\n", cfgPath))
			}
			msg += "\n" + helpStyle.Render("Tools from MCP appear as mcp_<server>_<tool> and are auto-registered. Try /mcp reload")
			m.appendRendered(msg)
			m.viewport.GotoBottom()
			return nil
		}
		// picker untuk semua MCP servers/tools — arrow+Enter
		var items []string
		for name, c := range mgr.Clients {
			for _, t := range c.Tools() {
				items = append(items, name+" / "+t.Name()+" — "+t.Description())
			}
			if len(c.Tools()) == 0 {
				items = append(items, name+" — (no tools) "+fmt.Sprintf("%s %v", c.Config.Command, c.Config.Args))
			}
		}
		m.openPicker(fmt.Sprintf("MCP — %d tools — Enter detail", len(mgr.AllTools())), items, func(sel string) tea.Cmd {
			m.appendSystem("MCP selected: " + sel)
			return nil
		}, true)
		return nil
	}
	if cmd == "reload" {
		if mgr != nil {
			mgr.Close()
		}
		newMgr := GetMCPManager()
		// For reload, we need to re-create manager via mcp.NewManager (global is same, so close and reload)
		// Simple: close and try to load again
		if newMgr != nil {
			newMgr.Close()
		}
		// Re-load via mcp package directly (need context)
		// Use background
		mgr2 := GetMCPManager()
		if mgr2 == nil {
			m.appendRendered(statusStyle.Render("MCP reload not available (no manager)"))
			m.viewport.GotoBottom()
			return nil
		}
		// Actually just report - full reload requires restart; we do best effort
		m.appendRendered(statusStyle.Render("MCP reload: restart pi-go to reload servers (close and reopen). Use /mcp list to check status."))
		m.viewport.GotoBottom()
		return nil
	}
	m.appendRendered(errorStyle.Render(fmt.Sprintf("Unknown /mcp subcommand: %s", args)) + "\n" + helpStyle.Render("Usage: /mcp [list|status|reload]"))
	m.viewport.GotoBottom()
	return nil
}

func handleTrust(m *Model, _ string) tea.Cmd {
	// show AGENTS.md load status
	cwd, _ := os.Getwd()
	var found []string
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md", ".agents.md"} {
		if _, err := os.Stat(filepath.Join(cwd, name)); err == nil {
			found = append(found, name)
		}
	}
	msg := headerStyle.Render(" Trust ") + "\n\n"
	if len(found) == 0 {
		msg += statusStyle.Render("No AGENTS.md found in cwd — project not trusted yet\n")
	} else {
		msg += fmt.Sprintf("Trusted files in %s:\n", cwd)
		for _, f := range found {
			msg += "  ✓ " + f + "\n"
		}
	}
	msg += "\n" + helpStyle.Render("AGENTS.md auto-loaded each turn via InstructionLoader")
	m.appendRendered(msg)
	m.viewport.GotoBottom()
	return nil
}

func handleImport(m *Model, args string) tea.Cmd {
	path := strings.TrimSpace(args)
	if path == "" {
		m.appendRendered(errorStyle.Render("Usage: /import <file.jsonl>"))
		m.viewport.GotoBottom()
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		m.appendRendered(errorStyle.Render(fmt.Sprintf("Import failed: %v", err)))
		m.viewport.GotoBottom()
		return nil
	}
	defer f.Close()

	msgs, err := session.ReadMessagesJSONL(f)
	if err != nil {
		m.appendRendered(errorStyle.Render(fmt.Sprintf("Import failed: %v", err)))
		m.viewport.GotoBottom()
		return nil
	}
	if len(msgs) == 0 {
		m.appendRendered(errorStyle.Render("No messages found in " + path))
		m.viewport.GotoBottom()
		return nil
	}

	m.switchToTree(session.BuildTreeFromMessages(msgs), "", fmt.Sprintf("Imported %d messages from %s", len(msgs), path))
	return nil
}

func handleNotImpl(m *Model, _ string) tea.Cmd {
	m.appendRendered(statusStyle.Render("This command is not yet implemented in pi-go. See /help for available commands."))
	m.viewport.GotoBottom()
	return nil
}

// helpers for auth.json

func piAgentDirTUI() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".pi", "agent")
}

func savePiAuth(provider, key, baseURL string) error {
	dir := piAgentDirTUI()
	if dir == "" {
		return fmt.Errorf("no home dir")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "auth.json")
	var m map[string]map[string]string
	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &m)
	}
	if m == nil {
		m = make(map[string]map[string]string)
	}
	rec := map[string]string{"type": "api_key", "key": key}
	if baseURL != "" {
		rec["baseUrl"] = baseURL
	} else if old, ok := m[strings.ToLower(provider)]; ok {
		if u, ok := old["baseUrl"]; ok {
			rec["baseUrl"] = u // preserve previously stored URL
		}
	}
	m[strings.ToLower(provider)] = rec
	out, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(path, out, 0600)
}

func removePiAuth(provider string) error {
	dir := piAgentDirTUI()
	if dir == "" {
		return fmt.Errorf("no home dir")
	}
	path := filepath.Join(dir, "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // nothing to remove
	}
	var m map[string]map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	delete(m, strings.ToLower(provider))
	out, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(path, out, 0600)
}

// --- favorites & default helpers ---

func favoritesPath() string {
	return filepath.Join(piAgentDirTUI(), "pi-go-favorites.json")
}

func loadFavorites() []string {
	data, err := os.ReadFile(favoritesPath())
	if err != nil {
		return nil
	}
	var obj struct {
		Favorites []string `json:"favorites"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	return obj.Favorites
}

func saveFavorites(favs []string) error {
	dir := piAgentDirTUI()
	if dir == "" {
		return fmt.Errorf("no home dir")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	obj := struct {
		Favorites []string `json:"favorites"`
	}{Favorites: favs}
	out, _ := json.MarshalIndent(obj, "", "  ")
	return os.WriteFile(favoritesPath(), out, 0600)
}

// loadKeyOverrides reads settings.json "keybindings": {action: key}.
// Returns alias map overrideKey -> canonicalKey used by the TUI switch.
func loadKeyOverrides() map[string]string {
	data, err := os.ReadFile(filepath.Join(piAgentDirTUI(), "settings.json"))
	if err != nil {
		return nil
	}
	var s struct {
		Keybindings map[string]string `json:"keybindings"`
	}
	if err := json.Unmarshal(data, &s); err != nil || len(s.Keybindings) == 0 {
		return nil
	}
	canonical := map[string]string{
		"quit":        "ctrl+c",
		"clear":       "ctrl+l",
		"palette":     "ctrl+p",
		"modelPicker": "ctrl+o",
		"thinkCycle":  "ctrl+t",
		"copy":        "y",
		"copyCode":    "ctrl+y",
		"pasteImage":  "ctrl+v",
		"toggleHelp":  "?",
	}
	alias := map[string]string{}
	for action, key := range s.Keybindings {
		c, ok := canonical[strings.TrimSpace(action)]
		if !ok {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(key))
		if k == "" {
			continue
		}
		alias[k] = c
	}
	return alias
}

func loadDefaultModel() (string, string) {
	data, err := os.ReadFile(filepath.Join(piAgentDirTUI(), "settings.json"))
	if err != nil {
		return "", ""
	}
	var s struct {
		DefaultProvider string `json:"defaultProvider"`
		DefaultModel    string `json:"defaultModel"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return "", ""
	}
	return s.DefaultProvider, s.DefaultModel
}

func saveDefaultModel(provider, model string) error {
	dir := piAgentDirTUI()
	if dir == "" {
		return fmt.Errorf("no home dir")
	}
	path := filepath.Join(dir, "settings.json")
	data, err := os.ReadFile(path)
	var raw map[string]interface{}
	if err == nil {
		json.Unmarshal(data, &raw)
	}
	if raw == nil {
		raw = make(map[string]interface{})
	}
	raw["defaultProvider"] = provider
	raw["defaultModel"] = model
	out, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0600)
}

func handleFavorite(m *Model, args string) tea.Cmd {
	parts := strings.Fields(args)
	if len(parts) == 0 || strings.EqualFold(parts[0], "list") {
		return handleFavoriteList(m, "")
	}
	action := strings.ToLower(parts[0])
	target := ""
	if len(parts) > 1 {
		target = strings.Join(parts[1:], " ")
		target = strings.ReplaceAll(target, " ", "/")
	}
	switch action {
	case "add":
		if target == "" {
			// add current model
			target = m.engine.Config.Model
			if m.engine.Config.Provider != nil && !strings.Contains(target, "/") {
				target = m.engine.Config.Provider.Name() + "/" + target
			}
		}
		favs := loadFavorites()
		for _, f := range favs {
			if strings.EqualFold(f, target) {
				m.appendRendered(statusStyle.Render(fmt.Sprintf("Already favorite: %s ♥", target)))
				m.viewport.GotoBottom()
				return nil
			}
		}
		favs = append(favs, target)
		if err := saveFavorites(favs); err != nil {
			m.appendRendered(errorStyle.Render(fmt.Sprintf("Failed to save favorite: %v", err)))
		} else {
			m.appendRendered(toolStyle.Render(fmt.Sprintf("♥ Added favorite: %s", target)) + "\n" + helpStyle.Render(fmt.Sprintf("Total favorites: %d — /favorites to list", len(favs))))
		}
		m.viewport.GotoBottom()
		return nil
	case "remove", "rm", "del", "delete":
		if target == "" {
			favs := loadFavorites()
			if len(favs) == 0 {
				m.appendRendered(statusStyle.Render("No favorites to remove"))
				m.viewport.GotoBottom()
				return nil
			}
			m.openPicker("Remove favorite — Enter to remove", favs, func(sel string) tea.Cmd {
				newFavs := []string{}
				for _, f := range favs {
					if !strings.EqualFold(f, sel) {
						newFavs = append(newFavs, f)
					}
				}
				saveFavorites(newFavs)
				m.appendSystem("Removed favorite: " + sel)
				return nil
			}, false)
			return nil
		}
		favs := loadFavorites()
		newFavs := []string{}
		removed := false
		for _, f := range favs {
			if strings.EqualFold(f, target) {
				removed = true
				continue
			}
			newFavs = append(newFavs, f)
		}
		if !removed {
			m.appendRendered(statusStyle.Render(fmt.Sprintf("Not in favorites: %s", target)))
		} else {
			saveFavorites(newFavs)
			m.appendRendered(toolStyle.Render(fmt.Sprintf("Removed favorite: %s", target)))
		}
		m.viewport.GotoBottom()
		return nil
	case "clear":
		saveFavorites(nil)
		m.appendRendered(statusStyle.Render("Cleared all favorites"))
		m.viewport.GotoBottom()
		return nil
	default:
		// treat args as model to add directly: /fav kimi-k2.6
		if len(parts) == 1 && !strings.Contains(action, " ") {
			// single arg without add/remove -> add
			target = parts[0]
			favs := loadFavorites()
			for _, f := range favs {
				if strings.EqualFold(f, target) {
					m.appendRendered(statusStyle.Render(fmt.Sprintf("Already favorite: %s ♥", target)))
					m.viewport.GotoBottom()
					return nil
				}
			}
			favs = append(favs, target)
			saveFavorites(favs)
			m.appendRendered(toolStyle.Render(fmt.Sprintf("♥ Added favorite: %s", target)))
			m.viewport.GotoBottom()
			return nil
		}
		m.appendRendered(errorStyle.Render("Usage: /favorite add <model> | /favorite remove <model> | /favorite list"))
		m.viewport.GotoBottom()
		return nil
	}
}

func handleFavoriteList(m *Model, _ string) tea.Cmd {
	favs := loadFavorites()
	if len(favs) == 0 {
		m.appendRendered(statusStyle.Render("No favorites yet.\n") + helpStyle.Render("Add: /favorite add kimi-k2.6  atau  /favorite add opencode-go/deepseek-v4-flash"))
		m.viewport.GotoBottom()
		return nil
	}
	defProv, defModel := loadDefaultModel()
	defFull := ""
	if defModel != "" {
		defFull = defProv + "/" + defModel
		if defProv == "" {
			defFull = defModel
		}
	}
	current := m.engine.Config.Model
	if m.engine.Config.Provider != nil && !strings.Contains(current, "/") {
		current = m.engine.Config.Provider.Name() + "/" + current
	}
	// picker — arrow+Enter untuk semua metode pilih
	m.openPicker("Favorites — Enter switch model, Esc close", favs, func(sel string) tea.Cmd {
		// bersihkan marker jika ada
		sel = strings.TrimSpace(strings.Split(sel, " ")[0])
		handleModel(m, sel)
		m.appendSystem("♥ favorite selected: " + sel + " — switching...")
		return nil
	}, false)
	// pre-select current if in favs
	for i, f := range favs {
		if strings.EqualFold(f, current) || strings.EqualFold(f, defFull) {
			m.picker.cursor = i
			break
		}
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- new Pi-style commands ---

func handleThink(m *Model, args string) tea.Cmd {
	level := strings.ToLower(strings.TrimSpace(args))
	valid := map[string]bool{
		"": true, "off": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true,
	}
	if !valid[level] {
		m.appendRendered(errorStyle.Render("Invalid thinking level. Use: off | minimal | low | medium | high | xhigh | max"))
		m.viewport.GotoBottom()
		return nil
	}
	if level == "" {
		// show picker
		items := []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}
		m.openPicker("Thinking effort — ↑/↓ + Enter", items, func(sel string) tea.Cmd {
			m.thinkingMode = sel
			m.appendSystem("Thinking: " + sel)
			return nil
		}, false)
		return nil
	}
	m.thinkingMode = level
	m.appendSystem("Thinking: " + level)
	return nil
}

func handleBg(m *Model, args string) tea.Cmd {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		m.appendRendered(errorStyle.Render("Usage: /bg <command> | /bg list | /bg cancel <id>"))
		m.viewport.GotoBottom()
		return nil
	}
	sub := strings.ToLower(parts[0])
	switch sub {
	case "list":
		if len(m.bgTasks) == 0 {
			m.appendRendered(statusStyle.Render("No background tasks"))
		} else {
			var sb strings.Builder
			sb.WriteString(headerStyle.Render(" Background tasks ") + "\n")
			for id, label := range m.bgTasks {
				sb.WriteString(fmt.Sprintf("  [%s] %s\n", id, label))
			}
			m.appendRendered(sb.String())
		}
		m.viewport.GotoBottom()
		return nil
	case "cancel":
		if len(parts) < 2 {
			m.appendRendered(errorStyle.Render("Usage: /bg cancel <id>"))
			m.viewport.GotoBottom()
			return nil
		}
		id := parts[1]
		if _, ok := m.bgTasks[id]; !ok {
			m.appendRendered(statusStyle.Render("No such background task: " + id))
			m.viewport.GotoBottom()
			return nil
		}
		delete(m.bgTasks, id)
		m.appendSystem("✓ cancelled background task " + id)
		return nil
	}
	// treat as command
	cmd := strings.Join(parts, " ")
	m.bgCounter++
	id := fmt.Sprintf("bg%d", m.bgCounter)
	m.bgTasks[id] = cmd
	go func() {
		c := exec.Command("bash", "-c", cmd)
		out, _ := c.CombinedOutput()
		preview := truncate(strings.TrimRight(string(out), "\n"), 200)
		m.appendSystem(fmt.Sprintf("[%s] done: %s", id, preview))
	}()
	m.appendSystem(fmt.Sprintf("▶ started background task %s: %s", id, cmd))
	return nil
}

func handleTools(m *Model, _ string) tea.Cmd {
	if m.engine == nil || m.engine.Config.Tools == nil {
		m.appendRendered(statusStyle.Render("No tools available"))
		m.viewport.GotoBottom()
		return nil
	}
	defs := m.engine.Config.Tools.Definitions()
	if len(defs) == 0 {
		m.appendRendered(statusStyle.Render("No tools registered"))
		m.viewport.GotoBottom()
		return nil
	}
	var sb strings.Builder
	sb.WriteString(headerStyle.Render(" Available Tools ") + "\n\n")
	for _, d := range defs {
		sb.WriteString(fmt.Sprintf("  %s — %s\n", footerAccentStyle.Render(d.Name), d.Description))
	}
	if mgr := GetMCPManager(); mgr != nil {
		all := mgr.AllTools()
		if len(all) > 0 {
			sb.WriteString("\n" + statusStyle.Render(fmt.Sprintf("MCP tools: %d", len(all))) + "\n")
		}
	}
	m.appendRendered(sb.String())
	m.viewport.GotoBottom()
	return nil
}

func handleStatus(m *Model, _ string) tea.Cmd {
	provName := "none"
	if m.engine != nil && m.engine.Config.Provider != nil {
		provName = m.engine.Config.Provider.Name()
	}
	wd, _ := os.Getwd()
	info := fmt.Sprintf(`%s
Model:    %s
Provider: %s
Workdir:  %s
Branch:   %s
Thinking: %s
Tokens:   %d
BG tasks: %d
`,
		headerStyle.Render(" Status "),
		m.engine.Config.Model,
		provName,
		wd,
		m.gitBranch,
		m.thinkingMode,
		m.tokens,
		len(m.bgTasks),
	)
	m.appendRendered(info)
	m.viewport.GotoBottom()
	return nil
}

func handleCd(m *Model, args string) tea.Cmd {
	path := strings.TrimSpace(args)
	if path == "" {
		m.appendRendered(errorStyle.Render("Usage: /cd <path>"))
		m.viewport.GotoBottom()
		return nil
	}
	if err := os.Chdir(path); err != nil {
		m.appendRendered(errorStyle.Render(fmt.Sprintf("cd failed: %v", err)))
		m.viewport.GotoBottom()
		return nil
	}
	wd, _ := os.Getwd()
	m.gitBranch = detectGitBranch()
	m.appendSystem("✓ cd " + wd)
	return nil
}
