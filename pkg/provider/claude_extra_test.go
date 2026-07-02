package provider_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/ctxdump/pkg/provider"
)

// TestClaudeSingleLineDump is a regression test: a single-line JSONL session file
// used to be misclassified as the legacy whole-file shape and parsed as empty.
func TestClaudeSingleLineDump(t *testing.T) {
	p := provider.NewClaudeProvider()
	path := filepath.Join("..", "..", "testdata", "claude", "single_line", "single-uuid.jsonl")

	conv, err := p.Dump(path, provider.Options{})
	if err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	if len(conv.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d: %+v", len(conv.Messages), conv.Messages)
	}
	if conv.Messages[0].Content != "only line" {
		t.Errorf("message 0 = %+v", conv.Messages[0])
	}
	if conv.ResumeID != "single-uuid" {
		t.Errorf("ResumeID = %q", conv.ResumeID)
	}
	if conv.Cwd != "/x/y" {
		t.Errorf("Cwd = %q", conv.Cwd)
	}
}

// TestClaudeInlineSidechainExcluded verifies that a transcript flagged
// isSidechain inline is not surfaced by List.
func TestClaudeInlineSidechainExcluded(t *testing.T) {
	p := provider.NewClaudeProvider()
	dir, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "claude", "inline_sidechain"))

	convs, err := p.List(provider.Options{CustomPath: dir})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	for _, c := range convs {
		if c.ID == "conv.jsonl" {
			t.Errorf("inline sidechain conv.jsonl should be excluded from List")
		}
	}
}

func TestClaudeToolRendering(t *testing.T) {
	p := provider.NewClaudeProvider()
	path := filepath.Join("..", "..", "testdata", "claude", "tools", "tools.jsonl")

	conv, err := p.Dump(path, provider.Options{})
	if err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	var all strings.Builder
	for _, m := range conv.Messages {
		all.WriteString(m.Content)
		all.WriteString("\n")
	}
	got := all.String()

	for _, want := range []string{
		"$ ls -la",
		"# list",
		"file listing",
		"[Tool Use: Edit main.go]",
		"-foo",
		"+bar",
		"[Tool Use: Write new.txt]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tool rendering missing %q in:\n%s", want, got)
		}
	}
}

// TestClaudeDumpRouting checks that Dump resolves a conversation by direct file
// path, by exact ID under a CustomPath, and by ID without extension via the List
// fallback.
func TestClaudeDumpRouting(t *testing.T) {
	p := provider.NewClaudeProvider()
	claudeDir := filepath.Join("..", "..", "testdata", "claude")

	// Direct file path.
	direct, err := p.Dump(filepath.Join(claudeDir, "mock_jsonl.jsonl"), provider.Options{})
	if err != nil {
		t.Fatalf("Dump by path failed: %v", err)
	}
	if len(direct.Messages) != 1 || direct.Messages[0].Content != "test message" {
		t.Errorf("direct dump messages = %+v", direct.Messages)
	}

	absDir, _ := filepath.Abs(claudeDir)
	opts := provider.Options{CustomPath: absDir}

	// Exact ID under CustomPath.
	byID, err := p.Dump("mock_jsonl.jsonl", opts)
	if err != nil {
		t.Fatalf("Dump by id failed: %v", err)
	}
	if byID.ResumeID != "mock-session-uuid" {
		t.Errorf("byID ResumeID = %q", byID.ResumeID)
	}

	// ID without extension, resolved via the List fallback.
	byStem, err := p.Dump("mock_jsonl", opts)
	if err != nil {
		t.Fatalf("Dump by stem failed: %v", err)
	}
	if byStem.ResumeID != "mock-session-uuid" {
		t.Errorf("byStem ResumeID = %q", byStem.ResumeID)
	}
}
