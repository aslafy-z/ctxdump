package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

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
   {{join .Names ", "}}{{"\t"}}{{.Usage}}{{if .Flags}}
     Flags:{{range .Flags}}
       {{.}}{{end}}{{end}}
{{end}}
`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "provider", Usage: "Filter by provider (codex, claude, gemini, antigravity)"},
			&cli.StringFlag{Name: "path", Usage: "Custom path to search for conversations"},
			&cli.StringFlag{Name: "sort", Value: "cwd", Usage: "Sort mode: cwd, date"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Value: "table", Usage: "Output format: table, json, jsonl, markdown, raw, plain, agent"},
			&cli.BoolFlag{Name: "interactive", Aliases: []string{"i"}, Usage: "Force interactive TUI mode"},
			&cli.BoolFlag{Name: "non-interactive", Aliases: []string{"n"}, Usage: "Disable interactive TUI mode"},
		},
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "Discover and list conversations",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					opts := provider.Options{CustomPath: cmd.String("path")}
					providers, err := getProviders(cmd.String("provider"))
					if err != nil {
						return err
					}

					convs := loadAndSortConversations(providers, opts, cmd.String("sort"))

					useTUI := isTTY()
					if cmd.Bool("interactive") {
						useTUI = true
					}
					if cmd.Bool("non-interactive") {
						useTUI = false
					}

					if useTUI {
						_, err := tui.Run(convs, "", false, opts, reg, "copy", cmd.String("output"))
						return err
					}

					printConversations(convs, cmd.String("output"))
					return nil
				},
			},
			{
				Name:  "search",
				Usage: "Search conversations",
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

					sortConversations(matches, cmd.String("sort"))

					if interactive {
						_, err := tui.Run(matches, query, true, opts, reg, "copy", cmd.String("output"))
						return err
					}

					printConversations(matches, cmd.String("output"))
					return nil
				},
			},
			{
				Name:  "dump",
				Usage: "Dump a specific conversation by ID or file path",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "max-tool-output", Value: 8000, Usage: "Max tool output bytes"},
					&cli.IntFlag{Name: "max-message-bytes", Value: 50000, Usage: "Max message bytes"},
					&cli.BoolFlag{Name: "full", Usage: "Do not truncate output"},
					&cli.BoolFlag{Name: "timestamps", Usage: "Include timestamps in agent output"},
					&cli.BoolFlag{Name: "include-thoughts", Usage: "Include thinking/commentary messages"},
				},
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
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "exec", Usage: "Custom execution template (e.g. 'editor {}' or 'editor {path}')"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() == 0 {
						return fmt.Errorf("missing <provider/editor> argument for resume")
					}

					var providerName string
					var idOrFile string

					// Check if first arg is a known provider
					if p, err := reg.Get(cmd.Args().Get(0)); err == nil {
						providerName = p.Name()
						if cmd.Args().Len() > 1 {
							idOrFile = cmd.Args().Get(1)
						}
					} else {
						idOrFile = cmd.Args().Get(0)
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
						if providerName == "" {
							return fmt.Errorf("missing <provider> argument for interactive/latest resume")
						}

						providers, err := getProviders(providerName)
						if err != nil {
							return err
						}

						convs := loadAndSortConversations(providers, opts, cmd.String("sort"))
						if len(convs) == 0 {
							return fmt.Errorf("no conversations found for provider %q", providerName)
						}

						if isTTY() && !cmd.Bool("non-interactive") {
							sel, err := tui.Run(convs, "", false, opts, reg, "resume", "")
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
					}

					var execCmd *exec.Cmd
					if cmd.String("exec") != "" {
						cmdStr := strings.ReplaceAll(cmd.String("exec"), "{}", selected.ID)
						cmdStr = strings.ReplaceAll(cmdStr, "{path}", selected.FilePath)

						parts := strings.Fields(cmdStr)
						if len(parts) == 0 {
							return fmt.Errorf("empty exec template")
						}
						execCmd = exec.Command(parts[0], parts[1:]...)
					} else {
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
				convs := loadAndSortConversations(providers, opts, "cwd")
				_, err = tui.Run(convs, "", false, opts, reg, "copy", "table")
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

func loadAndSortConversations(providers []provider.Provider, opts provider.Options, sortMode string) []models.Conversation {
	convs := loadConversations(providers, opts)
	sortConversations(convs, sortMode)
	return convs
}

func sortConversations(convs []models.Conversation, sortMode string) {
	currentCwd, _ := os.Getwd()
	sort.Slice(convs, func(i, j int) bool {
		if sortMode == "cwd" {
			scoreI := computeSortScore(convs[i], currentCwd)
			scoreJ := computeSortScore(convs[j], currentCwd)
			if scoreI != scoreJ {
				return scoreI > scoreJ
			}
		}
		return convs[i].UpdatedAt.After(convs[j].UpdatedAt)
	})
}

func findAndDumpConversation(providers []provider.Provider, idOrFile string, opts provider.Options) (models.Conversation, error) {
	for _, p := range providers {
		if conv, err := p.Dump(idOrFile, opts); err == nil {
			return conv, nil
		}
	}
	return models.Conversation{}, fmt.Errorf("conversation %q not found", idOrFile)
}

func cwdScore(current, target string) int {
	if target == "" {
		return -1
	}
	current = filepath.Clean(current)
	target = filepath.Clean(target)

	if current == target {
		return 10000
	}

	currParts := strings.Split(current, string(filepath.Separator))
	targParts := strings.Split(target, string(filepath.Separator))

	commonLen := 0
	for i := 0; i < len(currParts) && i < len(targParts); i++ {
		if currParts[i] == targParts[i] {
			commonLen += len(currParts[i]) + 1
		} else {
			break
		}
	}

	return (commonLen * 100) - len(target)
}

func computeSortScore(c models.Conversation, currentCwd string) float64 {
	prox := float64(cwdScore(currentCwd, c.Cwd))
	if prox < 0 {
		prox = 0
	}

	bonusDays := prox / 2000.0
	actualAgeDays := time.Since(c.UpdatedAt).Hours() / 24.0

	return bonusDays - actualAgeDays
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
