package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"pi/pkg/agent"
	piContext "pi/pkg/context"
	"pi/pkg/mcp"
	"pi/pkg/provider"
	"pi/pkg/provider/modelcatalog"
	"pi/pkg/rpc"
	"pi/pkg/session"
	"pi/pkg/tools"
	"pi/pkg/tui"
	"pi/pkg/types"
	"pi/pkg/update"
)

var globalMCPManager *mcp.Manager

// version is stamped at build time via -ldflags "-X main.version=vX.Y.Z".
// It is used by the self-updater and reported by `pi-go --version`.
var version = "dev"

// pendingUpdateNotice is set when a self-update replaced the on-disk binary
// during this launch; the TUI surfaces it in the chat log at startup.
var pendingUpdateNotice string

// isVersionOrHelpInvocation reports whether the user just asked for
// --version/-v/--help/-h, in which case we skip the network update check.
func isVersionOrHelpInvocation(cmd *cobra.Command, args []string) bool {
	if cmd.CalledAs() == "version" {
		return true
	}
	for _, a := range args {
		switch a {
		case "--version", "-v", "--help", "-h":
			return true
		}
	}
	return false
}

var (
	flagModel    string
	flagProvider string
	flagBaseURL  string
	flagAPIKey   string
	flagPrint    string
	flagMode     string
	flagNoMCP    bool
	flagSession  string
	flagContinue bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "pi",
		Short:   "Pi Coding Agent - lightweight native Go coding assistant",
		Long:    `Pi is a lightweight, high-performance coding agent with multi-provider LLM support, tool execution, TUI, print and RPC modes.`,
		Version: version,
		RunE:    runRoot,
	}

	rootCmd.Flags().StringVar(&flagModel, "model", "", "Model to use (e.g. openai/gpt-4o, anthropic/claude-3-5-sonnet, ollama/llama3, gemini/gemini-1.5-flash)")
	rootCmd.Flags().StringVar(&flagProvider, "provider", "", "Provider override (openai, anthropic, gemini, ollama, openrouter, groq, deepseek)")
	rootCmd.Flags().StringVar(&flagBaseURL, "base-url", "", "Custom base URL for OpenAI-compatible providers")
	rootCmd.Flags().StringVar(&flagAPIKey, "api-key", "", "API key (or set OPENAI_API_KEY/ANTHROPIC_API_KEY/GEMINI_API_KEY)")
	rootCmd.Flags().StringVarP(&flagPrint, "print", "p", "", "One-shot print mode: query as argument (alternatively pass as positional arg with -p)")
	rootCmd.Flags().StringVar(&flagMode, "mode", "", "Mode: interactive (default), print, rpc")
	rootCmd.Flags().BoolVar(&flagNoMCP, "no-mcp", false, "Disable MCP servers entirely (faster startup, fewer prompt tokens)")
	rootCmd.Flags().StringVar(&flagSession, "session", "", "Resume a saved session by id (see /resume)")
	rootCmd.Flags().BoolVar(&flagContinue, "continue", false, "Resume the most recent session")

	addCLICommands(rootCmd)

	// also accept -p as bool flag style: pi -p "query" -> cobra handles string
	// support --mode rpc explicitly
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runRoot(cmd *cobra.Command, args []string) error {
	// Self-update: silently check GitHub Releases and replace the on-disk
	// binary if a newer version exists (respects PI_NO_UPDATE + cooldown).
	// Skipped for --version/--help so those stay pure and fast.
	if !isVersionOrHelpInvocation(cmd, args) {
		if newVersion := update.CheckAndUpdate(version); newVersion != "" {
			pendingUpdateNotice = fmt.Sprintf("pi-go: updated to %s — this session still runs %s; restart pi-go to use the new version", newVersion, version)
		}
	}
	// Resolve print query: -p flag may be in args if passed as -p "query" without =
	// Cobra already handles --print string, but positional fallback:
	printQuery := flagPrint
	if printQuery == "" && len(args) > 0 {
		// check if -p was used as bool? For `pi -p "hello"` cobra puts "hello" in args when flag is defined as string expecting value
		// If mode is print or print flag set, treat args as query
		if flagMode == "print" {
			printQuery = strings.Join(args, " ")
		}
	}
	// Also support `pi -p` string value already captured; if printQuery still empty but args present and flagPrint was set via -p "x",
	// cobra will have consumed it, so nothing to do.
	// Support `pi "query"` without flags when mode not rpc? Not needed.

	switch strings.ToLower(flagMode) {
	case "rpc":
		return runRPCMode()
	case "print":
		if printQuery == "" {
			printQuery = strings.Join(args, " ")
		}
		if printQuery == "" {
			// also try to read from stdin? but require query
			return fmt.Errorf("print mode requires a query: pi --mode print \"your question\" or pi -p \"question\"")
		}
		return runPrintMode(printQuery)
	default:
		// Detect -p usage even without --mode
		if printQuery != "" {
			return runPrintMode(printQuery)
		}
		// If args contain query and no model? treat as print? No, default interactive ignores args
		// interactive TUI mode
		return runInteractiveMode()
	}
}

func buildEngine() (*agent.Engine, error) {
	model := flagModel
	if model == "" {
		model = os.Getenv("PI_MODEL")
	}
	// Jangan hardcode default di sini: dengan model kosong, ResolveProvider akan
	// memuat defaultProvider/defaultModel dari ~/.pi/agent/settings.json (interop
	// pi npm). Stealth/ox-alpha HANYA dipakai sebagai last-resort bila settings.json
	// tidak ada (lihat error path di bawah) — model itu sudah deprecate (404).
	// Jangan default ke openai/gpt-4o-mini — biarkan factory resolve
	cfg := provider.Config{
		Model:    model,
		Provider: flagProvider,
		BaseURL:  flagBaseURL,
		APIKey:   flagAPIKey,
	}
	pvd, modelName, err := provider.ResolveProvider(cfg)
	if err != nil {
		// Jika model berasal dari PI_MODEL env yang invalid, coba fallback ke settings npm pi (opencode-go/kimi)
		if flagModel == "" && os.Getenv("PI_MODEL") != "" {
			fallbackCfg := provider.Config{
				Provider: flagProvider,
				BaseURL:  flagBaseURL,
				APIKey:   flagAPIKey,
			}
			if pvd2, m2, err2 := provider.ResolveProvider(fallbackCfg); err2 == nil {
				fmt.Fprintf(os.Stderr, "warning: PI_MODEL=%q invalid (%v), fallback ke %s/%s dari ~/.pi/agent/settings.json\n", model, err, pvd2.Name(), m2)
				pvd, modelName, err = pvd2, m2, nil
			}
		}
	}
	// Last-resort built-in default: hanya dipakai bila tidak ada settings.json sama
	// sekali (fresh install). Kalau settings.json ada, ResolveProvider sudah memuat
	// default tersimpan di atas sehingga model deprecate tidak terpakai.
	if err != nil {
		fallbackCfg := cfg
		fallbackCfg.Model = "openrouter/stealth/ox-alpha"
		if pvd2, m2, err2 := provider.ResolveProvider(fallbackCfg); err2 == nil {
			pvd, modelName, err = pvd2, m2, nil
		}
	}
	if err != nil {
		// Beri hint yang lebih jelas + cara fix via /login atau env
		return nil, fmt.Errorf("failed to resolve provider for model %q: %w\n  Fix: pi-go --model opencode-go/kimi-k2.6 -p \"hi\"  atau  export OPENAI_API_KEY=sk-...  atau  /login openai <key> di TUI", model, err)
	}

	sess := session.NewTree()
	storage := session.NewStorage("")
	compactor := session.NewCompactor(40, pvd, modelName)
	// Token budget from the model catalog: compact once the history passes
	// ~75% of the model's context window (chars/4 estimate). Falls back to
	// message-count-only for unknown/custom models.
	if info, ok := modelcatalog.Lookup(pvd.Name(), modelName); ok && info.ContextWindow > 0 {
		compactor.MaxTokens = info.ContextWindow * 3 / 4
	}

	workspace, _ := os.Getwd()
	instrLoader := piContext.NewInstructionLoader(workspace)
	skillLoader := piContext.NewSkillLoader(workspace)
	promptLoader := piContext.NewPromptLoader(workspace)

	// Build tools registry + MCP servers (stdio).
	// MCP servers are spawned via npx (Node), which takes ~2.4s per launch.
	// Loading them synchronously here blocks every startup before the TUI
	// can even render, so they are loaded in the background instead and
	// registered into the (mutex-protected) registry once ready.
	reg := tools.DefaultRegistry()
	if flagNoMCP {
		fmt.Fprintln(os.Stderr, "[mcp] disabled (--no-mcp)")
	} else {
		mcpMgr := mcp.NewManager()
		globalMCPManager = mcpMgr
		tui.SetMCPManager(mcpMgr)
		go loadMCPServers(mcpMgr, reg)
	}

	eng := agent.NewEngine(agent.EngineConfig{
		Provider:          pvd,
		Model:             modelName,
		Tools:             reg,
		SessionTree:       sess,
		InstructionLoader: instrLoader,
		SkillLoader:       skillLoader,
		PromptLoader:      promptLoader,
		Compactor:         compactor,
		Listener:          &agent.DefaultEventListener{},
	})

	// ensure storage is kept for future session persistence (not used yet but available)
	_ = storage

	return eng, nil
}

// loadMCPServers starts configured MCP servers in the background with a
// bounded startup budget so a hung server can never block the agent.
// Tools are registered into the registry as soon as they become available.
func loadMCPServers(mgr *mcp.Manager, reg *tools.Registry) {
	tui.NotifyStatus("[mcp] loading servers in background...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	started, errs, _ := mgr.LoadAndStart(ctx)
	for _, e := range errs {
		tui.NotifyStatus("[mcp] " + e)
	}
	if started > 0 {
		for _, t := range mgr.AllTools() {
			reg.Register(t)
		}
		tui.NotifyStatus(fmt.Sprintf("[mcp] connected %d server(s), %d tools (ready)", started, len(mgr.AllTools())))
		return
	}
	if ctx.Err() == context.DeadlineExceeded {
		tui.NotifyStatus("[mcp] startup timed out after 30s; continuing without MCP tools")
	}
}

func runInteractiveMode() error {
	eng, err := buildEngine()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		// create engine without provider for offline demo; still launch TUI
		eng = agent.NewEngine(agent.EngineConfig{
			Model:       flagModel,
			SessionTree: session.NewTree(),
			Tools:       tools.DefaultRegistry(),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "starting in offline mode (no provider). Set API key to enable LLM.\n")
		}
	}
	defer func() {
		if globalMCPManager != nil {
			globalMCPManager.Close()
		}
	}()

	m := tui.NewModel(eng)
	tui.SetVersion(version)
	if pendingUpdateNotice != "" {
		m.AppendStartupNotice(pendingUpdateNotice)
	}
	if flagSession != "" {
		if err := m.LoadSessionByID(flagSession); err != nil {
			return fmt.Errorf("failed to resume session %q: %w", flagSession, err)
		}
	} else if flagContinue {
		if err := m.ContinueMostRecent(); err != nil {
			return err
		}
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	// Route async notices (MCP background load) into the live program instead
	// of stderr, which would corrupt the alternate screen.
	tui.AttachNotifier(func(msg tea.Msg) { p.Send(msg) })
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui failed: %w", err)
	}
	return nil
}

func runPrintMode(query string) error {
	eng, err := buildEngine()
	if err != nil {
		return err
	}
	defer func() {
		if globalMCPManager != nil {
			globalMCPManager.Close()
		}
	}()

	// Print mode: stream directly to stdout, with simple event listener
	ctx := context.Background()

	// Simple streaming listener that prints deltas directly
	eng.Config.Listener = &printListener{}

	fmt.Fprintf(os.Stderr, "Pi (model=%s) • query: %s\n\n", eng.Config.Model, query)
	if err := eng.RunTurn(ctx, query); err != nil {
		return fmt.Errorf("agent turn failed: %w", err)
	}
	fmt.Println()
	return nil
}

// printListener streams content deltas to stdout, and annotates tool calls
type printListener struct {
	agent.DefaultEventListener
}

func (p *printListener) OnContentDelta(delta string) {
	fmt.Print(delta)
}
func (p *printListener) OnThinkingDelta(delta string) {
	// Reasoning/thinking dikirim ke stderr agar stdout tetap bersih (bisa dipipe).
	fmt.Fprint(os.Stderr, delta)
}
func (p *printListener) OnToolExecutionStart(tool string, argsJSON string) {
	fmt.Printf("\n[tool: %s] %s\n", tool, truncatePrint(argsJSON, 200))
}
func (p *printListener) OnUsage(usage types.TokenUsage) {}
func (p *printListener) OnToolExecutionEnd(tool string, result string, isError bool) {
	if isError {
		fmt.Printf("[tool %s error]\n%s\n", tool, truncatePrint(result, 500))
	} else {
		fmt.Printf("[tool %s result] %s\n", tool, truncatePrint(result, 500))
	}
}
func (p *printListener) OnError(err error) {
	fmt.Fprintf(os.Stderr, "\n[error] %v\n", err)
}

func truncatePrint(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func runRPCMode() error {
	eng, err := buildEngine()
	if err != nil {
		// For RPC mode we allow missing provider initially; client can set_model later
		eng = agent.NewEngine(agent.EngineConfig{
			Model:       flagModel,
			SessionTree: session.NewTree(),
			Tools:       tools.DefaultRegistry(),
		})
		// try to set provider if possible; otherwise keep nil and let RPC handle set_model
		if eng.Config.Provider == nil && flagModel != "" {
			cfg := provider.Config{Model: flagModel, Provider: flagProvider, BaseURL: flagBaseURL, APIKey: flagAPIKey}
			if pvd, m, e2 := provider.ResolveProvider(cfg); e2 == nil {
				eng.Config.Provider = pvd
				eng.Config.Model = m
			} else {
				fmt.Fprintf(os.Stderr, "rpc server started without provider: %v (client should send set_model)\n", e2)
			}
		} else {
			fmt.Fprintf(os.Stderr, "rpc server started without provider: %v\n", err)
		}
	}

	defer func() {
		if globalMCPManager != nil {
			globalMCPManager.Close()
		}
	}()

	// Ensure we have a session tree
	if eng.Config.SessionTree == nil {
		eng.Config.SessionTree = session.NewTree()
	}

	srv, err := rpc.NewServer(rpc.ServerConfig{
		Session: eng.Config.SessionTree,
		Engine:  eng,
	})
	if err != nil {
		return fmt.Errorf("failed to create rpc server: %w", err)
	}

	// RPC protocol: JSONL over stdin/stdout, stderr for logs
	fmt.Fprintf(os.Stderr, "Pi RPC server listening on stdin/stdout (model=%s)\n", eng.Config.Model)
	return srv.Run(context.Background(), os.Stdin, os.Stdout)
}

// Ensure types import is used (for doc)
var _ = types.RoleUser
