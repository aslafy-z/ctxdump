package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/charmbracelet/x/term"
	"github.com/urfave/cli/v3"
	"github.com/user/ctxdump/pkg/formatter"
	"github.com/user/ctxdump/pkg/models"
	"github.com/user/ctxdump/pkg/provider"
	"github.com/user/ctxdump/pkg/tui"
)

var reg = provider.NewRegistry()

// listFormats are the output formats valid for list/search (which render a set
// of conversations, not a single transcript).
var listFormats = []string{"table", "json", "jsonl", "path"}

func main() {
	if err := newApp().Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// newApp builds the root CLI command tree. It is separated from main so tests can
// drive commands via cmd.Run without spawning a process.
func newApp() *cli.Command {
	return &cli.Command{
		Name:  "ctxdump",
		Usage: "A CLI tool to find and dump previous local AI assistant conversations.",
		CustomRootCommandHelpTemplate: `NAME:
   {{.Name}} - {{.Usage}}

USAGE:
   {{.Name}} [global options] command [command options]

GLOBAL OPTIONS:{{range .Flags}}
   {{.}}{{end}}

COMMANDS:{{range .Commands}}
   {{join .Names ", "}}{{"\t"}}{{.Usage}}
{{end}}
`,
		Flags: getCommonFlags(false, "table"),
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "Discover and list conversations",
				Flags: getCommonFlags(true, "table"),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					sf, so, err := parseSort(cmd)
					if err != nil {
						return err
					}
					opts := provider.Options{CustomPath: cmd.String("path")}
					providers, err := getProviders(cmd.String("provider"))
					if err != nil {
						return err
					}

					convs := loadAndSortConversations(providers, opts, sf, so)

					useTUI := isTTY()
					if cmd.Bool("interactive") {
						useTUI = true
					}
					if cmd.Bool("non-interactive") {
						useTUI = false
					}

					if useTUI {
						_, err := tui.Run(convs, "", false, opts, reg, "copy", cmd.String("output"), cmd.String("sort"), cmd.String("order"))
						return err
					}

					if err := validateListFormat(cmd.String("output")); err != nil {
						return err
					}
					printConversations(convs, cmd.String("output"))
					return nil
				},
			},
			{
				Name:  "search",
				Usage: "Search conversations",
				Flags: getCommonFlags(true, "table"),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					sf, so, err := parseSort(cmd)
					if err != nil {
						return err
					}

					query := cmd.Args().First()
					interactive := cmd.Bool("interactive")
					nonInteractive := cmd.Bool("non-interactive")

					if nonInteractive {
						interactive = false
					}

					if query == "" && !interactive {
						if isTTY() && !nonInteractive {
							interactive = true
						} else {
							return fmt.Errorf("missing <query> argument for search")
						}
					}

					opts := provider.Options{CustomPath: cmd.String("path")}
					providers, err := getProviders(cmd.String("provider"))
					if err != nil {
						return err
					}

					allConvs := loadConversations(providers, opts)
					var matches []models.Conversation
					lowerQuery := strings.ToLower(query)

					for _, c := range allConvs {
						if strings.Contains(strings.ToLower(c.Title), lowerQuery) {
							matches = append(matches, c)
							continue
						}

						p, err := reg.Get(c.Provider)
						if err != nil {
							continue
						}
						// Search the full transcript. Prefer the file path so codex
						// bodies (which cannot be re-found via List) still match.
						ref := c.FilePath
						if ref == "" {
							ref = c.ID
						}
						fullConv, err := p.Dump(ref, opts)
						if err != nil {
							continue
						}
						for _, m := range fullConv.Messages {
							if strings.Contains(strings.ToLower(m.Content), lowerQuery) {
								matches = append(matches, c)
								break
							}
						}
					}

					models.SortConversations(matches, sf, so)

					if interactive {
						_, err := tui.Run(matches, query, false, opts, reg, "copy", cmd.String("output"), cmd.String("sort"), cmd.String("order"))
						return err
					}

					if err := validateListFormat(cmd.String("output")); err != nil {
						return err
					}
					printConversations(matches, cmd.String("output"))
					return nil
				},
			},
			{
				Name:  "dump",
				Usage: "Dump a specific conversation by ID or file path",
				Flags: append(getCommonFlags(true, "agent"),
					&cli.IntFlag{Name: "max-tool-output", Value: 8000, Usage: "Max tool output bytes"},
					&cli.IntFlag{Name: "max-message-bytes", Value: 50000, Usage: "Max message bytes"},
					&cli.BoolFlag{Name: "full", Usage: "Do not truncate output"},
					&cli.BoolFlag{Name: "timestamps", Usage: "Include timestamps in agent output"},
					&cli.BoolFlag{Name: "include-thoughts", Usage: "Include thinking/commentary messages"},
				),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					format := cmd.String("output")
					if !formatter.IsValidFormat(format) {
						return fmt.Errorf("invalid output format %q for dump (valid: %s)", format, strings.Join(formatter.ValidFormats, ", "))
					}

					idOrFile := cmd.Args().First()
					if idOrFile == "" {
						return fmt.Errorf("missing <id-or-file> argument")
					}

					opts := provider.Options{CustomPath: cmd.String("path")}
					providers, err := getProviders(cmd.String("provider"))
					if err != nil {
						return err
					}

					found, err := findAndDumpConversation(providers, idOrFile, opts)
					if err != nil {
						return err
					}

					optsFormat := formatter.Options{
						Format:          format,
						MaxToolOutput:   int(cmd.Int("max-tool-output")),
						MaxMessageBytes: int(cmd.Int("max-message-bytes")),
						Full:            cmd.Bool("full"),
						Timestamps:      cmd.Bool("timestamps"),
						IncludeThoughts: cmd.Bool("include-thoughts"),
					}

					output, err := formatter.Format(found, optsFormat)
					if err != nil {
						return fmt.Errorf("error formatting: %w", err)
					}

					fmt.Println(output)
					return nil
				},
			},
			{
				Name:  "resume",
				Usage: "Resume a conversation in its provider's native editor",
				Flags: append(getCommonFlags(true, "table"),
					&cli.StringFlag{Name: "exec", Usage: "Custom execution template (e.g. 'editor {}' or 'editor {path}')"},
					&cli.BoolFlag{Name: "print-cmd", Usage: "Print the resolved command without executing it"},
				),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					var providerName string
					var idOrFile string
					var promptArgs []string

					if cmd.Args().Len() > 0 {
						// Check if first arg is a known provider
						if p, err := reg.Get(cmd.Args().Get(0)); err == nil {
							providerName = p.Name()
							if cmd.Args().Len() > 1 {
								idOrFile = cmd.Args().Get(1)
								promptArgs = cmd.Args().Slice()[2:]
							}
						} else {
							idOrFile = cmd.Args().Get(0)
							promptArgs = cmd.Args().Slice()[1:]
						}
					}

					var selected models.Conversation
					opts := provider.Options{CustomPath: cmd.String("path")}

					if idOrFile != "" {
						providers, err := getProviders(providerName)
						if err != nil {
							return err
						}

						selected, err = findAndDumpConversation(providers, idOrFile, opts)
						if err != nil {
							return err
						}
						providerName = selected.Provider
					} else {
						sf, so, err := parseSort(cmd)
						if err != nil {
							return err
						}
						providers, err := getProviders(providerName)
						if err != nil {
							return err
						}

						convs := loadAndSortConversations(providers, opts, sf, so)
						if len(convs) == 0 {
							if providerName != "" {
								return fmt.Errorf("no conversations found for provider %q", providerName)
							}
							return fmt.Errorf("no conversations found")
						}

						if isTTY() && !cmd.Bool("non-interactive") {
							sel, err := tui.Run(convs, "", false, opts, reg, "resume", "", cmd.String("sort"), cmd.String("order"))
							if err != nil {
								return fmt.Errorf("error running TUI: %w", err)
							}
							if sel == nil {
								return nil
							}
							selected = *sel
						} else {
							selected = convs[0]
						}
						providerName = selected.Provider
					}

					p, err := reg.Get(providerName)
					if err != nil {
						return err
					}
					var execCmd *exec.Cmd

					if rp, ok := p.(provider.Resumer); ok && cmd.String("exec") == "" {
						if selected.ResumeID == "" && selected.FilePath != "" {
							if full, err := p.Dump(selected.FilePath, opts); err == nil {
								selected = full
							}
						}
						spec, err := rp.ResumeSpec(selected, opts, promptArgs)
						if err != nil {
							return err
						}

						if cmd.Bool("print-cmd") {
							cmdStr := ""
							if spec.Dir != "" {
								cmdStr += fmt.Sprintf("cd %s && ", spec.Dir)
							}
							if len(spec.Env) > 0 {
								cmdStr += strings.Join(spec.Env, " ") + " "
							}
							cmdStr += spec.Command
							for _, a := range spec.Args {
								cmdStr += fmt.Sprintf(" %q", a)
							}
							fmt.Println(cmdStr)
							return nil
						}

						execCmd = exec.Command(spec.Command, spec.Args...)
						execCmd.Dir = spec.Dir
						execCmd.Env = append(os.Environ(), spec.Env...)
					} else if cmd.String("exec") != "" {
						// Substitute shell-escaped values and run through `sh -c` so
						// paths with spaces and shell metacharacters are handled safely.
						cmdStr := strings.ReplaceAll(cmd.String("exec"), "{path}", shellQuote(selected.FilePath))
						cmdStr = strings.ReplaceAll(cmdStr, "{}", shellQuote(selected.ID))

						if cmd.Bool("print-cmd") {
							fmt.Println(cmdStr)
							return nil
						}

						execCmd = exec.Command("sh", "-c", cmdStr)
					} else {
						if cmd.Bool("print-cmd") {
							cmdStr := providerName + " " + selected.ID
							fmt.Println(cmdStr)
							return nil
						}
						execCmd = exec.Command(providerName, selected.ID)
					}

					execCmd.Stdin = os.Stdin
					execCmd.Stdout = os.Stdout
					execCmd.Stderr = os.Stderr

					if err := execCmd.Run(); err != nil {
						// Propagate the child's own exit code instead of collapsing to 1.
						if exitErr, ok := err.(*exec.ExitError); ok {
							os.Exit(exitErr.ExitCode())
						}
						return fmt.Errorf("execution failed: %w", err)
					}
					return nil
				},
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// Default action when no command is provided
			if isTTY() && !cmd.Bool("non-interactive") {
				sf, so, err := parseSort(cmd)
				if err != nil {
					return err
				}
				opts := provider.Options{}
				providers, err := getProviders("")
				if err != nil {
					return err
				}
				convs := loadAndSortConversations(providers, opts, sf, so)
				_, err = tui.Run(convs, "", false, opts, reg, "copy", "table", cmd.String("sort"), cmd.String("order"))
				return err
			}
			return cli.ShowAppHelp(cmd)
		},
	}
}

// Helpers

func isTTY() bool {
	stat, _ := os.Stdout.Stat()
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func getProviders(providerName string) ([]provider.Provider, error) {
	if providerName != "" {
		p, err := reg.Get(providerName)
		if err != nil {
			return nil, err
		}
		return []provider.Provider{p}, nil
	}
	return reg.All(), nil
}

func loadConversations(providers []provider.Provider, opts provider.Options) []models.Conversation {
	var allConversations []models.Conversation
	for _, p := range providers {
		convs, err := p.List(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to list from %s: %v\n", p.Name(), err)
			continue
		}
		allConversations = append(allConversations, convs...)
	}
	return allConversations
}

func loadAndSortConversations(providers []provider.Provider, opts provider.Options, sf models.SortField, so models.SortOrder) []models.Conversation {
	convs := loadConversations(providers, opts)
	models.SortConversations(convs, sf, so)
	return convs
}

// parseSort validates and resolves the --sort/--order flags.
func parseSort(cmd *cli.Command) (models.SortField, models.SortOrder, error) {
	sf, err := getSortField(cmd.String("sort"))
	if err != nil {
		return "", "", err
	}
	so, err := getSortOrder(cmd.String("order"))
	if err != nil {
		return "", "", err
	}
	return sf, so, nil
}

func getSortField(sortFlag string) (models.SortField, error) {
	switch strings.ToLower(sortFlag) {
	case "", "score", "cwd":
		return models.SortFieldScore, nil
	case "date", "time", "updated":
		return models.SortFieldDate, nil
	case "path", "filepath":
		return models.SortFieldPath, nil
	default:
		return "", fmt.Errorf("invalid sort key %q (valid: date, path, score, cwd)", sortFlag)
	}
}

func getSortOrder(orderFlag string) (models.SortOrder, error) {
	switch strings.ToLower(orderFlag) {
	case "", "desc":
		return models.SortOrderDesc, nil
	case "asc":
		return models.SortOrderAsc, nil
	default:
		return "", fmt.Errorf("invalid sort order %q (valid: asc, desc)", orderFlag)
	}
}

func validateListFormat(format string) error {
	for _, f := range listFormats {
		if f == format {
			return nil
		}
	}
	return fmt.Errorf("invalid output format %q for list/search (valid: %s)", format, strings.Join(listFormats, ", "))
}

// shellQuote wraps s in single quotes so it survives `sh -c` unchanged.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func findAndDumpConversation(providers []provider.Provider, idOrFile string, opts provider.Options) (models.Conversation, error) {
	// Every provider's Dump succeeds on any readable path, so routing is a two-step
	// process: first trust the path's well-known storage location, then fall back to
	// whichever provider actually parses content out of the file (an empty result
	// means the file is not that provider's format).
	var fallback *models.Conversation
	if detected := detectProviderFromPath(providers, idOrFile); detected != nil {
		if conv, err := detected.Dump(idOrFile, opts); err == nil {
			if len(conv.Messages) > 0 {
				return conv, nil
			}
			c := conv
			fallback = &c
		}
	}
	for _, p := range providers {
		if conv, err := p.Dump(idOrFile, opts); err == nil {
			if len(conv.Messages) > 0 {
				return conv, nil
			}
			if fallback == nil {
				c := conv
				fallback = &c
			}
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return models.Conversation{}, fmt.Errorf("conversation %q not found", idOrFile)
}

// detectProviderFromPath matches a conversation file path against each provider's
// well-known storage layout and returns the owning provider, or nil if unknown.
func detectProviderFromPath(providers []provider.Provider, path string) provider.Provider {
	markers := map[string][]string{
		"claude":      {"/.claude/projects/", "/.claude/tasks/"},
		"codex":       {"/.codex/sessions/"},
		"antigravity": {"/.gemini/antigravity-ide/", "/.gemini/antigravity/"},
		"gemini":      {"/.gemini/tmp/"},
	}
	for _, p := range providers {
		for _, marker := range markers[p.Name()] {
			if strings.Contains(path, marker) {
				return p
			}
		}
	}
	return nil
}

func printConversations(convs []models.Conversation, format string) {
	switch format {
	case "path":
		for _, c := range convs {
			fmt.Println(c.FilePath)
		}
		return
	case "json":
		fmt.Print("[\n")
		for i, c := range convs {
			b, err := json.MarshalIndent(c, "  ", "  ")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to encode %s/%s: %v\n", c.Provider, c.ID, err)
				continue
			}
			fmt.Print("  " + string(b))
			if i < len(convs)-1 {
				fmt.Print(",\n")
			} else {
				fmt.Print("\n")
			}
		}
		fmt.Print("]\n")
		return
	case "jsonl":
		for _, c := range convs {
			b, err := json.Marshal(c)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to encode %s/%s: %v\n", c.Provider, c.ID, err)
				continue
			}
			fmt.Println(string(b))
		}
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	termWidth := 80
	if isTTY() {
		if wt, _, err := term.GetSize(os.Stdout.Fd()); err == nil && wt > 0 {
			termWidth = wt
		}
	}

	maxTimeLen := 0
	maxIdLen := 0
	for _, c := range convs {
		timeStr := formatter.HumanizeTime(c.UpdatedAt)
		idStr := c.Provider + "/" + c.ID
		if l := utf8.RuneCountInString(timeStr); l > maxTimeLen {
			maxTimeLen = l
		}
		if l := utf8.RuneCountInString(idStr); l > maxIdLen {
			maxIdLen = l
		}
	}

	for _, c := range convs {
		title := c.Title
		if title == "" {
			title = "No title"
		}
		title = strings.ReplaceAll(title, "\n", " ")

		timeStr := formatter.HumanizeTime(c.UpdatedAt)
		idStr := c.Provider + "/" + c.ID

		consumed := maxTimeLen + maxIdLen + 4
		maxTitle := termWidth - consumed

		if maxTitle > 10 && utf8.RuneCountInString(title) > maxTitle {
			title = models.TruncateRunes(title, maxTitle-3) + "..."
		}

		fmt.Fprintf(w, "%s\t%s\t%s\n", idStr, title, timeStr)
	}
	w.Flush()
}

func getCommonFlags(hidden bool, defaultOutput string) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "provider", Usage: "Filter by provider (codex, claude, gemini, antigravity)", Hidden: hidden},
		&cli.StringFlag{Name: "path", Usage: "Custom path to search for conversations", Hidden: hidden},
		&cli.StringFlag{Name: "sort", Value: "cwd", Usage: "Sort key: date, path, score, cwd", Hidden: hidden},
		&cli.StringFlag{Name: "order", Value: "desc", Usage: "Sort order: asc, desc", Hidden: hidden},
		&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Value: defaultOutput, Usage: "Output format: table, json, jsonl, markdown, raw, plain, agent", Hidden: hidden},
		&cli.BoolFlag{Name: "interactive", Aliases: []string{"i"}, Usage: "Force interactive TUI mode", Hidden: hidden},
		&cli.BoolFlag{Name: "non-interactive", Aliases: []string{"n"}, Usage: "Disable interactive TUI mode", Hidden: hidden},
	}
}
