package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/user/ctxdump/pkg/formatter"
	"github.com/user/ctxdump/pkg/models"
	"github.com/user/ctxdump/pkg/provider"
)

// newTestModel builds a model backed by a real bubbles list, mirroring Run(),
// without ever starting a tea.Program.
func newTestModel(convs []models.Conversation, action string) model {
	items := make([]list.Item, len(convs))
	for i, c := range convs {
		items[i] = item{conv: c}
	}
	m := model{
		list:          list.New(items, list.NewDefaultDelegate(), 80, 24),
		conversations: convs,
		registry:      provider.NewRegistry(),
		action:        action,
		copyFormat:    "agent",
		formats:       formatter.ValidFormats,
		sortBy:        "cwd",
		sortOrder:     "desc",
	}
	m.list.Filter = m.getFilterFunc()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return updated.(model)
}

// step applies a message and re-casts the result to model.
func step(m model, msg tea.Msg) (model, tea.Cmd) {
	u, c := m.Update(msg)
	return u.(model), c
}

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func sampleConvs() []models.Conversation {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	return []models.Conversation{
		{
			ID:        "id-alpha",
			Provider:  "codex",
			FilePath:  "/tmp/alpha.jsonl",
			Cwd:       "/home/user/projects/alpha",
			Title:     "Alpha session",
			UpdatedAt: base.Add(2 * time.Hour),
		},
		{
			ID:        "id-beta",
			Provider:  "claude",
			FilePath:  "/tmp/beta.jsonl",
			Cwd:       "/home/user/projects/beta",
			Title:     "Beta session",
			UpdatedAt: base.Add(1 * time.Hour),
		},
		{
			ID:        "id-gamma",
			Provider:  "gemini",
			FilePath:  "/tmp/gamma.jsonl",
			Cwd:       "/var/tmp/gamma",
			Title:     "Gamma session",
			UpdatedAt: base,
		},
	}
}

func TestSortCycle(t *testing.T) {
	m := newTestModel(sampleConvs(), "copy")

	// Move selection off index 0 so we can observe applySorting reselecting 0.
	m, _ = step(m, runeKey('j'))
	if m.list.Index() == 0 {
		t.Fatalf("expected selection to move off index 0 before sorting")
	}

	want := []string{"date", "path", "score"}
	for _, expected := range want {
		var next model
		next, _ = step(m, runeKey('s'))
		m = next
		if m.sortBy != expected {
			t.Fatalf("sortBy = %q, want %q", m.sortBy, expected)
		}
		if m.list.Index() != 0 {
			t.Fatalf("after sorting selection index = %d, want 0", m.list.Index())
		}
	}
}

func TestSortOrderToggle(t *testing.T) {
	m := newTestModel(sampleConvs(), "copy")
	if m.sortOrder != "desc" {
		t.Fatalf("initial sortOrder = %q, want desc", m.sortOrder)
	}

	m, _ = step(m, runeKey('S'))
	if m.sortOrder != "asc" {
		t.Fatalf("after first toggle sortOrder = %q, want asc", m.sortOrder)
	}

	m, _ = step(m, runeKey('S'))
	if m.sortOrder != "desc" {
		t.Fatalf("after second toggle sortOrder = %q, want desc", m.sortOrder)
	}
}

func TestQuitKeys(t *testing.T) {
	m := newTestModel(sampleConvs(), "copy")

	after, cmd := step(m, runeKey('q'))
	if cmd == nil {
		t.Fatal("expected non-nil cmd for 'q'")
	}
	if after.selected != nil {
		t.Fatal("expected selected to remain nil after 'q'")
	}

	after, cmd = step(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for ctrl+c")
	}
	if after.selected != nil {
		t.Fatal("expected selected to remain nil after ctrl+c")
	}
}

func TestResumeSelection(t *testing.T) {
	convs := sampleConvs()
	m := newTestModel(convs, "resume")

	// Move to the second item and select it via enter.
	m, _ = step(m, runeKey('j'))
	wantID := m.list.SelectedItem().(item).conv.ID

	after, cmd := step(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil quit cmd on enter")
	}
	if after.selected == nil {
		t.Fatal("expected selected to be set on enter")
	}
	if after.selected.ID != wantID {
		t.Fatalf("selected.ID = %q, want %q", after.selected.ID, wantID)
	}

	// 'c' behaves the same as enter.
	afterC, cmdC := step(m, runeKey('c'))
	if cmdC == nil {
		t.Fatal("expected non-nil quit cmd on 'c'")
	}
	if afterC.selected == nil || afterC.selected.ID != wantID {
		t.Fatalf("selected on 'c' = %+v, want ID %q", afterC.selected, wantID)
	}
}

func TestEmptyListNoPanic(t *testing.T) {
	m := newTestModel(nil, "resume")

	for _, msg := range []tea.Msg{
		runeKey('s'),
		runeKey('S'),
		tea.KeyMsg{Type: tea.KeyEnter},
		runeKey('q'),
	} {
		m, _ = step(m, msg)
	}

	if v := m.View(); v == "" {
		t.Fatal("expected non-empty View() output for empty list")
	}
}

func TestCopyFormatCycle(t *testing.T) {
	m := newTestModel(sampleConvs(), "copy")

	start := m.copyFormat
	seen := map[string]bool{start: true}

	// One press must advance to a different format.
	m, _ = step(m, runeKey('o'))
	if m.copyFormat == start {
		t.Fatalf("copyFormat did not change after one 'o' press (still %q)", start)
	}
	seen[m.copyFormat] = true

	// Continue until we have advanced len(ValidFormats) times in total; we
	// should have wrapped back to the starting format.
	for i := 1; i < len(formatter.ValidFormats); i++ {
		m, _ = step(m, runeKey('o'))
		seen[m.copyFormat] = true
	}
	if m.copyFormat != start {
		t.Fatalf("after %d presses copyFormat = %q, want wrap to %q",
			len(formatter.ValidFormats), m.copyFormat, start)
	}
	if len(seen) != len(formatter.ValidFormats) {
		t.Fatalf("cycle visited %d distinct formats, want %d", len(seen), len(formatter.ValidFormats))
	}
}

func TestTitleReflectsSortField(t *testing.T) {
	m := newTestModel(sampleConvs(), "copy")

	// Sorting to "date" should surface "Date" in the list title.
	m, _ = step(m, runeKey('s'))
	if m.sortBy != "date" {
		t.Fatalf("sortBy = %q, want date", m.sortBy)
	}
	if title := m.list.Title; !strings.Contains(title, "Date") {
		t.Fatalf("title = %q, want it to contain \"Date\"", title)
	}

	// The default/cwd case must render as "Score".
	fresh := newTestModel(sampleConvs(), "copy")
	fresh.updateTitle()
	if title := fresh.list.Title; !strings.Contains(title, "Score") {
		t.Fatalf("title = %q, want it to contain \"Score\"", title)
	}
}

func TestFilterFunc(t *testing.T) {
	m := newTestModel(sampleConvs(), "copy")
	f := m.getFilterFunc()

	// Single term matches only the target that contains it.
	ranks := f("foo", []string{"foo bar", "baz"})
	if len(ranks) != 1 || ranks[0].Index != 0 {
		t.Fatalf("single-term ranks = %+v, want one rank at index 0", ranks)
	}

	// Empty term returns all indices in order.
	targets := []string{"one", "two", "three"}
	all := f("", targets)
	if len(all) != len(targets) {
		t.Fatalf("empty-term ranks len = %d, want %d", len(all), len(targets))
	}
	for i, r := range all {
		if r.Index != i {
			t.Fatalf("empty-term rank[%d].Index = %d, want %d", i, r.Index, i)
		}
	}

	// Multi-word term requires every word to be present (AND semantics).
	multi := f("foo bar", []string{"foo bar baz", "foo only", "bar only"})
	if len(multi) != 1 || multi[0].Index != 0 {
		t.Fatalf("multi-word ranks = %+v, want one rank at index 0", multi)
	}

	// Null bytes in targets are stripped without panic and still match.
	nulled := f("foobar", []string{"foo\x00bar"})
	if len(nulled) != 1 || nulled[0].Index != 0 {
		t.Fatalf("null-byte ranks = %+v, want one rank at index 0", nulled)
	}
}
