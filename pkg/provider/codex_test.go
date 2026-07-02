package provider_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/ctxdump/pkg/provider"
)

func TestCodexRollout2026Dump(t *testing.T) {
	p := provider.NewCodexProvider()
	path := filepath.Join("..", "..", "testdata", "codexextra", "rollout2026.jsonl")

	conv, err := p.Dump(path, provider.Options{})
	if err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	if conv.ResumeID != "019f0860-2413-7282-b259-52d0c9fde043" {
		t.Errorf("ResumeID = %q", conv.ResumeID)
	}
	if conv.Cwd != "/home/u/proj" {
		t.Errorf("Cwd = %q", conv.Cwd)
	}

	if len(conv.Messages) != 5 {
		t.Fatalf("expected 5 messages (dedup collapses twin copies), got %d: %+v", len(conv.Messages), conv.Messages)
	}

	if conv.Messages[0].Role != "user" || conv.Messages[0].Content != "the task" {
		t.Errorf("message 0 = %+v", conv.Messages[0])
	}
	if conv.Messages[0].Timestamp.IsZero() {
		t.Errorf("message 0 timestamp should be populated")
	}
	if conv.Messages[1].Role != "assistant" || !conv.Messages[1].IsThought || conv.Messages[1].Content != "thinking..." {
		t.Errorf("message 1 should be reasoning thought, got %+v", conv.Messages[1])
	}
	if conv.Messages[2].Role != "tool" || conv.Messages[2].Content != "$ ls" {
		t.Errorf("message 2 should render exec_command, got %+v", conv.Messages[2])
	}
	if conv.Messages[3].Role != "tool" || conv.Messages[3].Content != "file1\nfile2" {
		t.Errorf("message 3 should be function_call_output, got %+v", conv.Messages[3])
	}
	if conv.Messages[4].Role != "assistant" || conv.Messages[4].Content != "done" {
		t.Errorf("message 4 = %+v", conv.Messages[4])
	}

	for _, m := range conv.Messages {
		if strings.Contains(m.Content, "gAAA") {
			t.Errorf("encrypted_content leaked into transcript: %q", m.Content)
		}
	}

	if conv.UpdatedAt.Format("2006-01-02T15:04:05Z") != "2026-06-27T09:19:20Z" {
		t.Errorf("UpdatedAt should be newest message timestamp, got %v", conv.UpdatedAt)
	}
}

func TestCodexLegacy2025Dump(t *testing.T) {
	p := provider.NewCodexProvider()
	path := filepath.Join("..", "..", "testdata", "codexextra", "legacy2025.jsonl")

	conv, err := p.Dump(path, provider.Options{})
	if err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	if conv.ResumeID != "5a2712de-0000-0000-0000-000000000000" {
		t.Errorf("ResumeID = %q", conv.ResumeID)
	}
	if len(conv.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(conv.Messages), conv.Messages)
	}
	if conv.Messages[0].Role != "user" || conv.Messages[0].Content != "hello 2025" {
		t.Errorf("message 0 = %+v", conv.Messages[0])
	}
	if conv.Messages[1].Role != "assistant" || conv.Messages[1].Content != "hi" {
		t.Errorf("message 1 = %+v", conv.Messages[1])
	}
}

// TestCodexLongLine proves the JSONL scanner buffer is raised well past bufio's
// 64KB default: a single tool-output line longer than 64KB must not truncate the
// rest of the file, so a marker on a later line is still parsed.
func TestCodexLongLine(t *testing.T) {
	marker := "UNIQUE-TAIL-MARKER-abc123"
	lines := []map[string]interface{}{
		{
			"timestamp": "2026-06-27T09:00:00Z",
			"type":      "session_meta",
			"payload":   map[string]interface{}{"id": "long-id", "cwd": "/tmp/long"},
		},
		{
			"timestamp": "2026-06-27T09:00:01Z",
			"type":      "response_item",
			"payload": map[string]interface{}{
				"type":   "function_call_output",
				"output": strings.Repeat("x", 70000),
			},
		},
		{
			"timestamp": "2026-06-27T09:00:02Z",
			"type":      "event_msg",
			"payload":   map[string]interface{}{"type": "agent_message", "message": marker},
		},
	}

	var sb strings.Builder
	for _, l := range lines {
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "long.jsonl")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := provider.NewCodexProvider()
	conv, err := p.Dump(path, provider.Options{})
	if err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	found := false
	for _, m := range conv.Messages {
		if m.Content == marker {
			found = true
		}
	}
	if !found {
		t.Errorf("tail marker %q not parsed; long line likely truncated the scanner", marker)
	}
}
