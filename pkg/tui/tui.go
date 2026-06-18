package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/user/ctxdump/pkg/formatter"
	"github.com/user/ctxdump/pkg/models"
	"github.com/user/ctxdump/pkg/provider"
)

var docStyle = lipgloss.NewStyle().Margin(1, 2)

type item struct {
	conv models.Conversation
}

func (i item) Title() string { 
	title := i.conv.Title
	if title == "" {
		title = i.conv.ID
	}
	
	if title == i.conv.Snippet || i.conv.Snippet == "" || strings.HasPrefix(i.conv.Snippet, strings.TrimSuffix(title, "...")) {
		if i.conv.Snippet != "" {
			title = strings.ReplaceAll(i.conv.Snippet, "\n", " ")
		}
		return title
	}
	
	greyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	snippet := strings.ReplaceAll(i.conv.Snippet, "\n", " ")
	return title + greyStyle.Render(" - " + snippet)
}
func (i item) Description() string { 
	path := i.conv.FilePath
	if i.conv.Cwd != "" {
		path = i.conv.Cwd
	}
	return fmt.Sprintf("[%s] %s - %s", i.conv.Provider, formatter.HumanizeTime(i.conv.UpdatedAt), path)
}
func (i item) FilterValue() string { return i.conv.Title + " " + i.conv.Provider + " " + i.conv.Snippet }

type model struct {
	list          list.Model
	conversations []models.Conversation
	opts          provider.Options
	registry      *provider.Registry
	startInFilter bool
	status        string
	err           error
	action        string
	selected      *models.Conversation
	copyFormat    string
	formats       []string
}

func (m model) Init() tea.Cmd {
	if m.startInFilter {
		return func() tea.Msg {
			return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}
		}
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "d", "delete", "x", "backspace":
			i, ok := m.list.SelectedItem().(item)
			if ok {
				os.Remove(i.conv.FilePath)
				m.list.RemoveItem(m.list.Index())
				cmd := m.list.NewStatusMessage(fmt.Sprintf("Deleted '%s'", i.Title()))
				return m, cmd
			}

		case "enter", "c":
			i, ok := m.list.SelectedItem().(item)
			if ok {
				if m.action == "resume" {
					m.selected = &i.conv
					return m, tea.Quit
				}

				p, err := m.registry.Get(i.conv.Provider)
				if err != nil {
					m.err = fmt.Errorf("failed to get provider: %v", err)
					return m, tea.Quit
				}
				
				fullConv, err := p.Dump(i.conv.FilePath, m.opts)
				if err != nil {
					m.err = fmt.Errorf("failed to dump conversation: %v", err)
					return m, tea.Quit
				}
				
				md, err := formatter.Format(fullConv, formatter.Options{Format: m.copyFormat})
				if err != nil {
					m.err = fmt.Errorf("failed to format conversation: %v", err)
					return m, tea.Quit
				}
				
				if err := clipboard.WriteAll(md); err != nil {
					m.err = fmt.Errorf("clipboard error: %v", err)
					return m, tea.Quit
				}
				
				m.status = fmt.Sprintf("Copied '%s' to clipboard (Output: %s)!", i.Title(), m.copyFormat)
				return m, tea.Quit
			}

		case "o", "f":
			if m.action != "resume" {
				// Cycle format
				idx := -1
				for i, f := range m.formats {
					if f == m.copyFormat {
						idx = i
						break
					}
				}
				nextIdx := (idx + 1) % len(m.formats)
				m.copyFormat = m.formats[nextIdx]
				m.list.Title = fmt.Sprintf("Select a conversation to copy (Output: %s)", m.copyFormat)
				return m, nil
			}
		}
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.status != "" {
		return docStyle.Render(m.status + "\n\nPress any key to exit.")
	}
	return docStyle.Render(m.list.View())
}


// Run starts the Bubble Tea UI for searching and copying/resuming conversations.
// action can be "copy" or "resume". It returns the selected conversation if action is "resume".
func Run(conversations []models.Conversation, initialQuery string, startInFilter bool, opts provider.Options, reg *provider.Registry, action string, initialCopyFormat string) (*models.Conversation, error) {
	items := make([]list.Item, len(conversations))
	for i, c := range conversations {
		items[i] = item{conv: c}
	}

	delegate := list.NewDefaultDelegate()
	mList := list.New(items, delegate, 0, 0)

	copyFormat := "agent"
	validFormats := formatter.ValidFormats
	if initialCopyFormat == "plain" {
		initialCopyFormat = "text"
	}
	for _, f := range validFormats {
		if f == initialCopyFormat {
			copyFormat = f
			break
		}
	}

	m := model{
		list:          mList,
		conversations: conversations,
		opts:          opts,
		registry:      reg,
		startInFilter: startInFilter,
		action:        action,
		copyFormat:    copyFormat,
		formats:       validFormats,
	}
	if action == "resume" {
		m.list.Title = "Select a conversation to resume"
	} else {
		m.list.Title = fmt.Sprintf("Select a conversation to copy (Output: %s)", m.copyFormat)
	}
	m.list.AdditionalShortHelpKeys = func() []key.Binding {
		if action == "resume" {
			return []key.Binding{
				key.NewBinding(key.WithKeys("c", "enter"), key.WithHelp("c/enter", "resume")),
				key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d/del", "hide")),
			}
		}
		return []key.Binding{
			key.NewBinding(key.WithKeys("c", "enter"), key.WithHelp("c/enter", "copy")),
			key.NewBinding(key.WithKeys("o", "f"), key.WithHelp("o/f", "output")),
			key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d/del", "hide")),
		}
	}
	m.list.AdditionalFullHelpKeys = func() []key.Binding {
		if action == "resume" {
			return []key.Binding{
				key.NewBinding(key.WithKeys("c", "enter"), key.WithHelp("c/enter", "resume")),
				key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d/delete", "hide/delete from disk")),
			}
		}
		return []key.Binding{
			key.NewBinding(key.WithKeys("c", "enter"), key.WithHelp("c/enter", "copy")),
			key.NewBinding(key.WithKeys("o", "f"), key.WithHelp("o/f", "cycle output")),
			key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d/delete", "hide/delete from disk")),
		}
	}

	m.list.KeyMap.CursorUp.SetKeys("up", "k", "ctrl+k", "ctrl+p")
	m.list.KeyMap.CursorDown.SetKeys("down", "j", "ctrl+j", "ctrl+n")

	// If query is provided, we could pre-filter or just rely on the pre-filtered matches.
	// Since matches are already pre-filtered, the list will just display them.

	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("error running program: %v", err)
	}
	
	if fm, ok := finalModel.(model); ok {
		if fm.err != nil {
			return nil, fm.err
		}
		if fm.status != "" {
			fmt.Println(fm.status)
		}
		return fm.selected, nil
	}
	return nil, nil
}
