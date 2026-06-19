package provider

import (
	"path/filepath"
	"testing"
)

func TestExtractClaudeMeta(t *testing.T) {
	tests := []struct {
		name          string
		filePath      string
		expectTitle   string
		expectSnippet string
		expectCwd     string
	}{
		{
			name:          "JSONL with CWD",
			filePath:      "../../testdata/claude/mock_jsonl.jsonl",
			expectTitle:   "Test JSONL",
			expectSnippet: "test message",
			expectCwd:     "/my/correct/path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			absPath, _ := filepath.Abs(tc.filePath)
			title, snippet, cwd := extractClaudeMeta(absPath)
			
			if title != tc.expectTitle {
				t.Errorf("expected title %q, got %q", tc.expectTitle, title)
			}
			if snippet != tc.expectSnippet {
				t.Errorf("expected snippet %q, got %q", tc.expectSnippet, snippet)
			}
			if cwd != tc.expectCwd {
				t.Errorf("expected cwd %q, got %q", tc.expectCwd, cwd)
			}
		})
	}
}

func TestClaudeProviderListPathFallback(t *testing.T) {
	p := NewClaudeProvider()
	
	// Create a dummy option pointing to our testdata
	absPath, _ := filepath.Abs("../../testdata/claude")
	opts := Options{CustomPath: absPath}
	
	convs, err := p.List(opts)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	found := false
	for _, c := range convs {
		if c.ID == "mock_jsonl.jsonl" {
			found = true
			if c.Cwd != "/my/correct/path" {
				t.Errorf("expected Cwd to be '/my/correct/path', got %q", c.Cwd)
			}
		}
	}
	
	if !found {
		t.Errorf("did not find mock_jsonl.jsonl in List output")
	}
}
