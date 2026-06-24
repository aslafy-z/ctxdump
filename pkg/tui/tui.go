package tui

import (
	"fmt"
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
func (i item) FilterValue() string { 
	if i.conv.SearchContent != "" {
		return i.conv.Title + " " + i.conv.Provider + " " + i.conv.Snippet + " " + i.conv.SearchContent
	}
	return i.conv.Title + " " + i.conv.Provider + " " + i.conv.Snippet 
}

type startLoadingMsg struct{}

type allContentLoadedMsg struct {
	contents map[string]string
}

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
	sortBy        string
	sortOrder     string
}

func (m model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.startInFilter {
		cmds = append(cmds, func() tea.Msg {
			return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}
		})
	}
	cmds = append(cmds, func() tea.Msg {
		return startLoadingMsg{}
	})
	return tea.Batch(cmds...)
}

func loadAllContentCmd(registry *provider.Registry, opts provider.Options, convs []models.Conversation) tea.Cmd {
	return func() tea.Msg {
		contents := make(map[string]string)
		for _, conv := range convs {
			p, err := registry.Get(conv.Provider)
			if err != nil {
				continue
			}
			fullConv, err := p.Dump(conv.FilePath, opts)
			if err != nil {
				continue
			}
			var contentBuilder strings.Builder
			for _, msg := range fullConv.Messages {
				contentBuilder.WriteString(msg.Content)
				contentBuilder.WriteString(" ")
			}
			key := conv.Provider + "/" + conv.ID
			contents[key] = contentBuilder.String()
		}
		return allContentLoadedMsg{contents: contents}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case startLoadingMsg:
		if len(m.conversations) > 0 {
			// Copy conversations so we don't hold references to the UI model's slice
			convs := make([]models.Conversation, len(m.conversations))
			copy(convs, m.conversations)
			return m, loadAllContentCmd(m.registry, m.opts, convs)
		}

	case allContentLoadedMsg:
		items := m.list.Items()
		newItems := make([]list.Item, len(items))
		for i, itm := range items {
			typedItm := itm.(item)
			key := typedItm.conv.Provider + "/" + typedItm.conv.ID
			if content, ok := msg.contents[key]; ok {
				typedItm.conv.SearchContent = content
			}
			newItems[i] = typedItm
		}
		for i, conv := range m.conversations {
			key := conv.Provider + "/" + conv.ID
			if content, ok := msg.contents[key]; ok {
				m.conversations[i].SearchContent = content
			}
		}
		cmd := m.list.SetItems(newItems)
		return m, cmd

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		if m.list.FilterState() == list.Filtering {
			break
		}

		switch msg.String() {
		case "q":
			return m, tea.Quit

		case "d", "delete", "x", "backspace":
			i, ok := m.list.SelectedItem().(item)
			if ok {
				m.list.RemoveItem(m.list.Index())
				cmd := m.list.NewStatusMessage(fmt.Sprintf("Hidden '%s'", i.Title()))
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
				m.updateTitle()
				return m, nil
			}

		case "s":
			switch m.sortBy {
			case "date":
				m.sortBy = "path"
			case "path":
				m.sortBy = "score"
			case "score", "cwd":
				m.sortBy = "date"
			default:
				m.sortBy = "date"
			}
			cmd := m.applySorting()
			m.updateTitle()
			return m, tea.Batch(cmd, m.list.NewStatusMessage(fmt.Sprintf("Sorted by %s (%s)", m.sortBy, m.sortOrder)))

		case "S":
			if m.sortOrder == "asc" {
				m.sortOrder = "desc"
			} else {
				m.sortOrder = "asc"
			}
			cmd := m.applySorting()
			m.updateTitle()
			return m, tea.Batch(cmd, m.list.NewStatusMessage(fmt.Sprintf("Sorted by %s (%s)", m.sortBy, m.sortOrder)))
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
func Run(conversations []models.Conversation, initialQuery string, startInFilter bool, opts provider.Options, reg *provider.Registry, action string, initialCopyFormat string, sortBy string, sortOrder string) (*models.Conversation, error) {
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
		sortBy:        sortBy,
		sortOrder:     sortOrder,
	}

	// We use a custom filter function to clean targets of null bytes (to avoid sahilm/fuzzy panic),
	// strip MatchedIndexes (to prevent DefaultDelegate out-of-bounds highlighting panic),
	// and preserve the custom sort order when filtering.
	m.list.Filter = m.getFilterFunc()

	m.updateTitle()

	m.list.AdditionalShortHelpKeys = func() []key.Binding {
		if action == "resume" {
			return []key.Binding{
				key.NewBinding(key.WithKeys("c", "enter"), key.WithHelp("c/enter", "resume")),
				key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
				key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "order")),
				key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d/del", "hide")),
			}
		}
		return []key.Binding{
			key.NewBinding(key.WithKeys("c", "enter"), key.WithHelp("c/enter", "copy")),
			key.NewBinding(key.WithKeys("o", "f"), key.WithHelp("o/f", "output")),
			key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
			key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "order")),
			key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d/del", "hide")),
		}
	}
	m.list.AdditionalFullHelpKeys = func() []key.Binding {
		if action == "resume" {
			return []key.Binding{
				key.NewBinding(key.WithKeys("c", "enter"), key.WithHelp("c/enter", "resume")),
				key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort field")),
				key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "sort order")),
				key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d/delete", "hide")),
			}
		}
		return []key.Binding{
			key.NewBinding(key.WithKeys("c", "enter"), key.WithHelp("c/enter", "copy")),
			key.NewBinding(key.WithKeys("o", "f"), key.WithHelp("o/f", "cycle output")),
			key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort field")),
			key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "sort order")),
			key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d/delete", "hide")),
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

func (m *model) applySorting() tea.Cmd {
	var sf models.SortField
	switch m.sortBy {
	case "date":
		sf = models.SortFieldDate
	case "path":
		sf = models.SortFieldPath
	case "score", "cwd":
		sf = models.SortFieldScore
	default:
		sf = models.SortFieldDate
	}

	var so models.SortOrder
	if m.sortOrder == "asc" {
		so = models.SortOrderAsc
	} else {
		so = models.SortOrderDesc
	}

	models.SortConversations(m.conversations, sf, so)

	items := make([]list.Item, len(m.conversations))
	for i, c := range m.conversations {
		items[i] = item{conv: c}
	}
	m.list.Filter = m.getFilterFunc()
	cmd := m.list.SetItems(items)
	m.list.Select(0)
	return cmd
}

func (m *model) updateTitle() {
	var sortStr string
	switch m.sortBy {
	case "date":
		sortStr = "Date"
	case "path":
		sortStr = "Path"
	case "score", "cwd":
		sortStr = "Score"
	default:
		sortStr = m.sortBy
	}

	orderStr := "Desc"
	if m.sortOrder == "asc" {
		orderStr = "Asc"
	}

	if m.action == "resume" {
		m.list.Title = fmt.Sprintf("Select a conversation to resume [Sort: %s %s]", sortStr, orderStr)
	} else {
		m.list.Title = fmt.Sprintf("Select a conversation to copy (Output: %s) [Sort: %s %s]", m.copyFormat, sortStr, orderStr)
	}
}

func (m model) getFilterFunc() list.FilterFunc {
	return func(term string, targets []string) []list.Rank {
		if term == "" {
			ranks := make([]list.Rank, len(targets))
			for i := range targets {
				ranks[i] = list.Rank{Index: i}
			}
			return ranks
		}

		words := strings.Fields(strings.ToLower(term))
		var ranks []list.Rank
		for i, t := range targets {
			tLower := strings.ToLower(strings.ReplaceAll(t, "\x00", ""))
			matches := true
			for _, w := range words {
				if !strings.Contains(tLower, w) {
					matches = false
					break
				}
			}
			if matches {
				ranks = append(ranks, list.Rank{
					Index: i,
				})
			}
		}
		return ranks
	}
}
