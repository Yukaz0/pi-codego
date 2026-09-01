package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"pi/pkg/mcp"
	"pi/pkg/session"
	"pi/pkg/update"
)

// addCLICommands registers the non-interactive utility subcommands that
// mirror in-TUI features, so users who are still outside the TUI (or want
// to script) get short commands instead of long incantations:
//
//	pi-go update [--check]   self-update / version check   (TUI: /update)
//	pi-go sessions [list]    saved sessions                (TUI: /resume)
//	pi-go config             where config lives + defaults (TUI: /settings)
//	pi-go doctor             environment health report
func addCLICommands(root *cobra.Command) {
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newSessionsCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newDoctorCmd())
}

// latestFn is stubbed in tests to keep the suite hermetic (no GitHub calls).
var latestFn = update.Latest

// --- update ---

func newUpdateCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check GitHub Releases and replace the binary now",
		Long:  `Force-checks the latest pi-go release (ignoring the startup cooldown) and atomically replaces the on-disk binary when a newer version exists. The running session keeps the old version until restarted. Use --check to only report.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if check {
				latest, err := latestFn()
				if err != nil {
					return err
				}
				switch {
				case version == "dev" || strings.HasPrefix(version, "dev-"):
					fmt.Fprintf(out, "development build (%s); latest release is %s\n", version, latest)
				case update.IsNewer(latest, version):
					fmt.Fprintf(out, "update available: %s → %s (run: pi-go update)\n", version, latest)
				default:
					fmt.Fprintf(out, "pi-go is already on the latest version (%s)\n", version)
				}
				return nil
			}
			to, err := update.Update(version)
			if err != nil {
				return err
			}
			if to == version {
				fmt.Fprintf(out, "pi-go is already on the latest version (%s)\n", version)
				return nil
			}
			fmt.Fprintf(out, "pi-go updated %s → %s — restart pi-go to run the new version\n", version, to)
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only report whether a newer release exists; do not install")
	return cmd
}

// --- sessions ---

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List saved sessions",
		Long:  `Lists saved sessions newest-modified first, with the id needed by 'pi-go --session <id>'.`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 && args[0] != "list" {
				return fmt.Errorf("unknown sessions subcommand %q (try: pi-go sessions)", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			st := session.NewStorage("")
			list := st.ListSessions()
			if len(list) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "no saved sessions in %s\n", st.SessionDir)
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tUPDATED\tMSGS\tNAME / FIRST MESSAGE")
			for _, s := range list {
				label := s.Name
				if label == "" {
					label = firstLine(s.FirstMsg, 48)
				}
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", s.ID, humanAge(s.Modified), s.MessageCnt, label)
			}
			if err := w.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nresume with: pi-go --session <id>   (or: pi-go --continue)\n")
			return nil
		},
	}
	return cmd
}

func firstLine(s string, max int) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

func humanAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// --- config ---

func piAgentDir() string {
	if d := os.Getenv("PI_GO_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent")
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show config file locations and current defaults",
		Long:  `Prints where pi-go keeps auth, settings, MCP servers and sessions, plus the effective default provider/model from settings.json. API keys are listed by provider name only and never printed.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			dir := piAgentDir()
			fmt.Fprintf(out, "config dir:  %s\n", dir)
			fmt.Fprintf(out, "auth:        %s\n", exists(filepath.Join(dir, "auth.json")))
			fmt.Fprintf(out, "settings:    %s\n", exists(filepath.Join(dir, "settings.json")))
			fmt.Fprintf(out, "sessions:    %s\n", exists(session.NewStorage("").SessionDir))
			if _, p, err := mcp.LoadConfig(); err == nil && p != "" {
				fmt.Fprintf(out, "mcp config:  %s\n", p)
			} else {
				fmt.Fprintln(out, "mcp config:  none found (searched .pi/mcp.json, .mcp.json, ~/.pi/agent/mcp.json)")
			}
			prov, model := defaultFromSettings(dir)
			if prov == "" {
				fmt.Fprintln(out, "default:     (none — pick one with /model + Ctrl+D in the TUI)")
			} else {
				fmt.Fprintf(out, "default:     %s/%s\n", prov, model)
			}
			if keys := authProviders(dir); len(keys) > 0 {
				fmt.Fprintf(out, "keys saved:  %s\n", strings.Join(keys, ", "))
			}
			return nil
		},
	}
	return cmd
}

func exists(p string) string {
	if p == "" {
		return "n/a"
	}
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return p + " (missing)"
}

func defaultFromSettings(dir string) (string, string) {
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return "", ""
	}
	var s struct {
		DefaultProvider string `json:"defaultProvider"`
		DefaultModel    string `json:"defaultModel"`
	}
	if json.Unmarshal(data, &s) != nil {
		return "", ""
	}
	return s.DefaultProvider, s.DefaultModel
}

// authProviders lists provider names that have a stored key — values never leave the file.
func authProviders(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// --- doctor ---

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Report environment health (version, update, config, sessions)",
		Long:  `Quick checks: installed version vs latest release, config files present, default model set, stored keys, session count, MCP servers configured.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
			fmt.Fprintf(w, "version\t%s\t(%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)

			if latest, err := latestFn(); err != nil {
				fmt.Fprintf(w, "update check\tFAIL\t%v\n", err)
			} else if update.IsNewer(latest, version) {
				fmt.Fprintf(w, "update check\tOUTDATED\trun: pi-go update (%s → %s)\n", version, latest)
			} else {
				fmt.Fprintf(w, "update check\tOK\tlatest (%s)\n", latest)
			}

			dir := piAgentDir()
			prov, model := defaultFromSettings(dir)
			if prov == "" {
				fmt.Fprintln(w, "default model\tWARN\tnot set — /model then Ctrl+D in the TUI")
			} else {
				fmt.Fprintf(w, "default model\tOK\t%s/%s\n", prov, model)
			}

			if keys := authProviders(dir); len(keys) == 0 {
				fmt.Fprintln(w, "api keys\tWARN\tnone in auth.json — use /login or env vars")
			} else {
				fmt.Fprintf(w, "api keys\tOK\t%s\n", strings.Join(keys, ", "))
			}

			st := session.NewStorage("")
			fmt.Fprintf(w, "sessions\tOK\t%d saved in %s\n", len(st.ListSessions()), st.SessionDir)

			if cfg, p, err := mcp.LoadConfig(); err == nil && p != "" {
				fmt.Fprintf(w, "mcp\tOK\t%d server(s) in %s\n", len(cfg.MCPServers), p)
			} else {
				fmt.Fprintln(w, "mcp\tINFO\tno config (optional)")
			}
			return w.Flush()
		},
	}
	return cmd
}
