package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/x/term"
	"github.com/urfave/cli/v3"
	"github.com/user/ctxdump/pkg/formatter"
	"github.com/user/ctxdump/pkg/models"
	"github.com/user/ctxdump/pkg/provider"
	"github.com/user/ctxdump/pkg/tui"
)

var reg = provider.NewRegistry()

func main() {
	cmd := &cli.Command{
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
		Flags: getCommonFlags(false),
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "Discover and list conversations",
				Flags: getCommonFlags(true),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					opts := provider.Options{CustomPath: cmd.String("path")}
					providers, err := getProviders(cmd.String("provider"))
					if err != nil {
						return err
					}

					convs := loadAndSortConversations(providers, opts, cmd.String("sort"), cmd.String("order"))

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

					printConversations(convs, cmd.String("output"))
					return nil
				},
			},
			{
				Name:  "search",
				Usage: "Search conversations",
				Flags: getCommonFlags(true),
				Action: func(ctx context.Context, cmd *cli.Command) error {
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

						p, _ := reg.Get(c.Provider)
						fullConv, err := p.Dump(c.ID, opts)
						if err == nil {
							found := false
							for _, m := range fullConv.Messages {
								if strings.Contains(strings.ToLower(m.Content), lowerQuery) {
									found = true
									break
								}
							}
							if found {
								matches = append(matches, c)
							}
						}
					}

					models.SortConversations(matches, getSortField(cmd.String("sort")), getSortOrder(cmd.String("order")))

					if interactive {
						_, err := tui.Run(matches, query, false, opts, reg, "copy", cmd.String("output"), cmd.String("sort"), cmd.String("order"))
						return err
					}

					printConversations(matches, cmd.String("output"))
					return nil
				},
			},
			{
				Name:  "dump",
				Usage: "Dump a specific conversation by ID or file path",
				Flags: append(getCommonFlags(true),
					&cli.IntFlag{Name: "max-tool-output", Value: 8000, Usage: "Max tool output bytes"},
					&cli.IntFlag{Name: "max-message-bytes", Value: 50000, Usage: "Max message bytes"},
					&cli.BoolFlag{Name: "full", Usage: "Do not truncate output"},
					&cli.BoolFlag{Name: "timestamps", Usage: "Include timestamps in agent output"},
					&cli.BoolFlag{Name: "include-thoughts", Usage: "Include thinking/commentary messages"},
				),
				Action: func(ctx context.Context, cmd *cli.Command) error {
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
						Format:          cmd.String("output"),
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
				Flags: append(getCommonFlags(true),
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
						providers, err := getProviders(providerName)
						if err != nil {
							return err
						}

						convs := loadAndSortConversations(providers, opts, cmd.String("sort"), cmd.String("order"))
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

					p, _ := reg.Get(providerName)
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
						cmdStr := strings.ReplaceAll(cmd.String("exec"), "{}", selected.ID)
						cmdStr = strings.ReplaceAll(cmdStr, "{path}", selected.FilePath)

						if cmd.Bool("print-cmd") {
							fmt.Println(cmdStr)
							return nil
						}

						parts := strings.Fields(cmdStr)
						if len(parts) == 0 {
							return fmt.Errorf("empty exec template")
						}
						execCmd = exec.Command(parts[0], parts[1:]...)
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
						return fmt.Errorf("execution failed: %w", err)
					}
					return nil
				},
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// Default action when no command is provided
			if isTTY() && !cmd.Bool("non-interactive") {
				opts := provider.Options{}
				providers, err := getProviders("")
				if err != nil {
					return err
				}
				convs := loadAndSortConversations(providers, opts, cmd.String("sort"), cmd.String("order"))
				_, err = tui.Run(convs, "", false, opts, reg, "copy", "table", cmd.String("sort"), cmd.String("order"))
				return err
			}
			return cli.ShowAppHelp(cmd)
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
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

func loadAndSortConversations(providers []provider.Provider, opts provider.Options, sortMode string, sortOrder string) []models.Conversation {
	convs := loadConversations(providers, opts)
	models.SortConversations(convs, getSortField(sortMode), getSortOrder(sortOrder))
	return convs
}

func getSortField(sortFlag string) models.SortField {
	switch strings.ToLower(sortFlag) {
	case "date", "time", "updated":
		return models.SortFieldDate
	case "path", "filepath":
		return models.SortFieldPath
	case "score", "cwd":
		return models.SortFieldScore
	default:
		return models.SortFieldScore // default fallback
	}
}

func getSortOrder(orderFlag string) models.SortOrder {
	if strings.ToLower(orderFlag) == "asc" {
		return models.SortOrderAsc
	}
	return models.SortOrderDesc
}

func findAndDumpConversation(providers []provider.Provider, idOrFile string, opts provider.Options) (models.Conversation, error) {
	// Every provider's Dump succeeds on any readable path, so when the argument is
	// a file path we route by its location to avoid the wrong provider claiming it.
	if detected := detectProviderFromPath(providers, idOrFile); detected != nil {
		if conv, err := detected.Dump(idOrFile, opts); err == nil {
			return conv, nil
		}
	}
	for _, p := range providers {
		if conv, err := p.Dump(idOrFile, opts); err == nil {
			return conv, nil
		}
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
	if format == "json" {
		fmt.Print("[\n")
		for i, c := range convs {
			b, _ := json.MarshalIndent(c, "  ", "  ")
			fmt.Print("  " + string(b))
			if i < len(convs)-1 {
				fmt.Print(",\n")
			} else {
				fmt.Print("\n")
			}
		}
		fmt.Print("]\n")
		return
	}
	if format == "jsonl" {
		for _, c := range convs {
			b, _ := json.Marshal(c)
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
		if len(timeStr) > maxTimeLen {
			maxTimeLen = len(timeStr)
		}
		if len(idStr) > maxIdLen {
			maxIdLen = len(idStr)
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

		if maxTitle > 10 && len(title) > maxTitle {
			title = title[:maxTitle-3] + "..."
		}

		fmt.Fprintf(w, "%s\t%s\t%s\n", idStr, title, timeStr)
	}
	w.Flush()
}

func getCommonFlags(hidden bool) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "provider", Usage: "Filter by provider (codex, claude, gemini, antigravity)", Hidden: hidden},
		&cli.StringFlag{Name: "path", Usage: "Custom path to search for conversations", Hidden: hidden},
		&cli.StringFlag{Name: "sort", Value: "cwd", Usage: "Sort key: date, path, score, cwd", Hidden: hidden},
		&cli.StringFlag{Name: "order", Value: "desc", Usage: "Sort order: asc, desc", Hidden: hidden},
		&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Value: "table", Usage: "Output format: table, json, jsonl, markdown, raw, plain, agent", Hidden: hidden},
		&cli.BoolFlag{Name: "interactive", Aliases: []string{"i"}, Usage: "Force interactive TUI mode", Hidden: hidden},
		&cli.BoolFlag{Name: "non-interactive", Aliases: []string{"n"}, Usage: "Disable interactive TUI mode", Hidden: hidden},
	}
}
