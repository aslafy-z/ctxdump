package provider

import (
	"strings"
	"testing"
)

func TestUnifiedDiff(t *testing.T) {
	got := UnifiedDiff("a.txt", "old line", "new line")
	want := "--- a.txt\n+++ a.txt\n@@ @@\n-old line\n+new line"
	if got != want {
		t.Errorf("UnifiedDiff:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderToolUseEdit(t *testing.T) {
	block := map[string]interface{}{
		"name": "Edit",
		"input": map[string]interface{}{
			"file_path":  "main.go",
			"old_string": "foo",
			"new_string": "bar",
		},
	}
	got := renderToolUse(block)
	for _, want := range []string{"[Tool Use: Edit main.go]", "--- main.go", "-foo", "+bar"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderToolUse(Edit) missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderToolUseBashAndWrite(t *testing.T) {
	bash := renderToolUse(map[string]interface{}{
		"name":  "Bash",
		"input": map[string]interface{}{"command": "ls -la", "description": "list"},
	})
	if !strings.Contains(bash, "$ ls -la") || !strings.Contains(bash, "# list") {
		t.Errorf("renderToolUse(Bash) = %q", bash)
	}

	write := renderToolUse(map[string]interface{}{
		"name":  "Write",
		"input": map[string]interface{}{"file_path": "x.txt", "content": "hello"},
	})
	if !strings.Contains(write, "[Tool Use: Write x.txt]") || !strings.Contains(write, "hello") {
		t.Errorf("renderToolUse(Write) = %q", write)
	}
}

func TestRenderCodexTool(t *testing.T) {
	exec := renderCodexTool(map[string]interface{}{
		"type":      "function_call",
		"name":      "exec_command",
		"arguments": `{"cmd":"git status"}`,
	})
	if exec != "$ git status" {
		t.Errorf("function_call exec = %q", exec)
	}

	patch := renderCodexTool(map[string]interface{}{
		"type":  "custom_tool_call",
		"name":  "apply_patch",
		"input": "*** Begin Patch\n*** Update File: a\n",
	})
	if !strings.Contains(patch, "apply_patch") || !strings.Contains(patch, "Begin Patch") {
		t.Errorf("custom_tool_call apply_patch = %q", patch)
	}

	mcp := renderCodexTool(map[string]interface{}{
		"type":       "mcp_tool_call_end",
		"invocation": map[string]interface{}{"server": "grafana", "tool": "list_datasources"},
	})
	if mcp != "[MCP Call: grafana/list_datasources]" {
		t.Errorf("mcp_tool_call_end = %q", mcp)
	}

	if got := renderCodexTool(map[string]interface{}{"type": "reasoning"}); got != "" {
		t.Errorf("non-tool payload should render empty, got %q", got)
	}
}
