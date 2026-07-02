package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/user/ctxdump/pkg/models"
	"github.com/user/ctxdump/pkg/provider"
)

// capture swaps os.Stdout for a pipe, runs fn, and returns whatever fn wrote to
// stdout along with fn's error. The command helpers print to os.Stdout, and
// newApp uses the package-global reg, so swapping stdout before Run is enough to
// capture command output. Subtests using this must run serially (os.Stdout is
// process-global); do not call t.Parallel().
func capture(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b), runErr
}

// run drives the CLI in-process with the given args (program name is prepended).
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return capture(t, func() error {
		return newApp().Run(context.Background(), append([]string{"ctxdump"}, args...))
	})
}

const (
	claudeJSONL = "../../testdata/claude/mock_jsonl.jsonl"
	codexPath   = "../../testdata/codex"
	claudePath  = "../../testdata/claude"
)

func TestDumpDefaultFormatIsAgent(t *testing.T) {
	out, err := run(t, "dump", claudeJSONL, "-n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(out, "---") {
		t.Errorf("expected output to start with ---, got %q", out[:min(20, len(out))])
	}
	for _, want := range []string{"format: agent-handoff-v1", `source: "claude"`, "test message"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

func TestDumpFormats(t *testing.T) {
	t.Run("markdown", func(t *testing.T) {
		out, err := run(t, "dump", claudeJSONL, "-o", "markdown", "-n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "# ") {
			t.Errorf("expected markdown heading in output")
		}
	})

	t.Run("json", func(t *testing.T) {
		out, err := run(t, "dump", claudeJSONL, "-o", "json", "-n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatalf("output is not valid JSON object: %v", err)
		}
		if !strings.Contains(out, "Provider") {
			t.Errorf("expected JSON to contain Provider field")
		}
	})

	t.Run("text", func(t *testing.T) {
		out, err := run(t, "dump", claudeJSONL, "-o", "text", "-n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "[USER]") {
			t.Errorf("expected text output to contain [USER]")
		}
	})

	t.Run("plain alias behaves like text", func(t *testing.T) {
		out, err := run(t, "dump", claudeJSONL, "-o", "plain", "-n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "[USER]") {
			t.Errorf("expected plain output to contain [USER]")
		}
	})

	t.Run("path", func(t *testing.T) {
		out, err := run(t, "dump", claudeJSONL, "-o", "path", "-n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != claudeJSONL+"\n" {
			t.Errorf("expected %q, got %q", claudeJSONL+"\n", out)
		}
	})

	t.Run("raw", func(t *testing.T) {
		out, err := run(t, "dump", claudeJSONL, "-o", "raw", "-n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("expected non-empty raw output")
		}
	})
}

func TestDumpInvalidFormat(t *testing.T) {
	_, err := run(t, "dump", claudeJSONL, "-o", "table", "-n")
	if err == nil {
		t.Fatal("expected error for invalid dump format")
	}
	if !strings.Contains(err.Error(), `invalid output format "table" for dump`) {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDumpMissingArg(t *testing.T) {
	_, err := run(t, "dump", "-n")
	if err == nil {
		t.Fatal("expected error for missing id-or-file")
	}
	if !strings.Contains(err.Error(), "missing <id-or-file>") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestListFormats(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		out, err := run(t, "list", "-n", "--provider", "codex", "--path", codexPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "codex/mock1.json") {
			t.Errorf("expected table to contain codex/mock1.json, got:\n%s", out)
		}
	})

	t.Run("json", func(t *testing.T) {
		out, err := run(t, "list", "-n", "--provider", "codex", "--path", codexPath, "-o", "json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(strings.TrimSpace(out), "[") {
			t.Errorf("expected JSON array, got:\n%s", out)
		}
		var arr []map[string]any
		if err := json.Unmarshal([]byte(out), &arr); err != nil {
			t.Fatalf("output is not a valid JSON array: %v", err)
		}
	})

	t.Run("jsonl", func(t *testing.T) {
		out, err := run(t, "list", "-n", "--provider", "codex", "--path", codexPath, "-o", "jsonl")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("line is not valid JSON: %q: %v", line, err)
			}
		}
	})

	t.Run("path", func(t *testing.T) {
		out, err := run(t, "list", "-n", "--provider", "codex", "--path", codexPath, "-o", "path")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "testdata/codex/mock1.json") {
			t.Errorf("expected path output to contain testdata/codex/mock1.json, got:\n%s", out)
		}
	})
}

func TestListInvalidFormat(t *testing.T) {
	_, err := run(t, "list", "-n", "--path", codexPath, "-o", "agent")
	if err == nil {
		t.Fatal("expected error for invalid list format")
	}
	if !strings.Contains(err.Error(), `invalid output format "agent" for list/search`) {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestListInvalidSortAndOrder(t *testing.T) {
	t.Run("sort", func(t *testing.T) {
		_, err := run(t, "list", "-n", "--path", codexPath, "--sort", "bogus")
		if err == nil {
			t.Fatal("expected error for invalid sort key")
		}
		if !strings.Contains(err.Error(), "invalid sort key") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("order", func(t *testing.T) {
		_, err := run(t, "list", "-n", "--path", codexPath, "--order", "sideways")
		if err == nil {
			t.Fatal("expected error for invalid sort order")
		}
		if !strings.Contains(err.Error(), "invalid sort order") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestSearchMatchesBody(t *testing.T) {
	t.Run("codex", func(t *testing.T) {
		out, err := run(t, "search", "Hello codex", "-n", "--provider", "codex", "--path", codexPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "mock1.json") {
			t.Errorf("expected search to match mock1.json via body, got:\n%s", out)
		}
	})

	t.Run("claude", func(t *testing.T) {
		out, err := run(t, "search", "test message", "-n", "--provider", "claude", "--path", claudePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "mock_jsonl.jsonl") {
			t.Errorf("expected search to match mock_jsonl.jsonl via body, got:\n%s", out)
		}
	})
}

func TestGetSortField(t *testing.T) {
	cases := []struct {
		in   string
		want models.SortField
	}{
		{"date", models.SortFieldDate},
		{"cwd", models.SortFieldScore},
		{"score", models.SortFieldScore},
		{"", models.SortFieldScore},
		{"path", models.SortFieldPath},
	}
	for _, c := range cases {
		got, err := getSortField(c.in)
		if err != nil {
			t.Errorf("getSortField(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("getSortField(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := getSortField("bogus"); err == nil {
		t.Error("expected error for bogus sort field")
	}
}

func TestGetSortOrder(t *testing.T) {
	cases := []struct {
		in   string
		want models.SortOrder
	}{
		{"asc", models.SortOrderAsc},
		{"desc", models.SortOrderDesc},
		{"", models.SortOrderDesc},
	}
	for _, c := range cases {
		got, err := getSortOrder(c.in)
		if err != nil {
			t.Errorf("getSortOrder(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("getSortOrder(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := getSortOrder("x"); err == nil {
		t.Error("expected error for invalid sort order")
	}
}

func TestValidateListFormat(t *testing.T) {
	for _, f := range []string{"table", "json", "jsonl", "path"} {
		if err := validateListFormat(f); err != nil {
			t.Errorf("validateListFormat(%q) unexpected error: %v", f, err)
		}
	}
	for _, f := range []string{"markdown", "agent"} {
		if err := validateListFormat(f); err == nil {
			t.Errorf("validateListFormat(%q) expected error", f)
		}
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("a b"); got != `'a b'` {
		t.Errorf(`shellQuote("a b") = %q, want %q`, got, `'a b'`)
	}
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Errorf(`shellQuote("it's") = %q, want %q`, got, `'it'\''s'`)
	}
}

func TestDetectProviderFromPath(t *testing.T) {
	all := reg.All()
	cases := []struct {
		path string
		want string
	}{
		{"/home/u/.codex/sessions/x.jsonl", "codex"},
		{"/home/u/.claude/projects/p/x.jsonl", "claude"},
		{"/home/u/.gemini/tmp/h/logs.json", "gemini"},
	}
	for _, c := range cases {
		got := detectProviderFromPath(all, c.path)
		if got == nil {
			t.Errorf("detectProviderFromPath(%q) = nil, want %q", c.path, c.want)
			continue
		}
		if got.Name() != c.want {
			t.Errorf("detectProviderFromPath(%q) = %q, want %q", c.path, got.Name(), c.want)
		}
	}
	if got := detectProviderFromPath(all, "foo.txt"); got != nil {
		t.Errorf("detectProviderFromPath(\"foo.txt\") = %q, want nil", got.Name())
	}
}

func TestFindAndDumpConversationPrefersNonEmpty(t *testing.T) {
	conv, err := findAndDumpConversation(reg.All(), claudeJSONL, provider.Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conv.Provider != "claude" {
		t.Errorf("expected provider claude, got %q", conv.Provider)
	}
	if len(conv.Messages) == 0 {
		t.Errorf("expected non-empty messages")
	}
}

func TestResumePrintCmdNative(t *testing.T) {
	out, err := run(t, "resume", "codex", "mock2.jsonl", "--path", codexPath, "--print-cmd", "-n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `codex "resume"`) {
		t.Errorf("expected native resume command, got:\n%s", out)
	}
	if !strings.Contains(out, "123") {
		t.Errorf("expected resume id in output, got:\n%s", out)
	}
}

func TestResumePrintCmdExecTemplating(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/a b.jsonl"
	if err := os.WriteFile(path, []byte(`{"type":"message","role":"user","content":"hi"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	out, err := run(t, "resume", path, "--exec", "echo {path}", "--print-cmd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	quoted := "'" + path + "'"
	if !strings.Contains(out, quoted) {
		t.Errorf("expected exec output to contain %q, got:\n%s", quoted, out)
	}
}
