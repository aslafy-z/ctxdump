package provider_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/ctxdump/pkg/provider"
)

func TestGeminiLogsArrayDump(t *testing.T) {
	p := provider.NewGeminiProvider()
	path := filepath.Join("..", "..", "testdata", "gemini", "logs_array", "logs.json")

	conv, err := p.Dump(path, provider.Options{})
	if err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	if conv.ResumeID != "c2c2aaaa-bbbb-cccc-dddd-eeeeffff0000" {
		t.Errorf("ResumeID = %q", conv.ResumeID)
	}
	if len(conv.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(conv.Messages), conv.Messages)
	}
	if conv.Messages[0].Role != "user" || conv.Messages[0].Content != "Hey" {
		t.Errorf("message 0 = %+v", conv.Messages[0])
	}
	if conv.Messages[1].Role != "assistant" || conv.Messages[1].Content != "Hi there" {
		t.Errorf("message 1 = %+v", conv.Messages[1])
	}
	if conv.UpdatedAt.Format("2006-01-02T15:04:05Z") != "2025-07-31T09:40:05Z" {
		t.Errorf("UpdatedAt should be last message timestamp, got %v", conv.UpdatedAt)
	}
}

// TestGeminiChatsDump exercises the chats JSONL shape where the last $set snapshot
// wins. Only the last snapshot's messages are scanned, so the workspace block that
// lived in the first snapshot is not reflected in Cwd here.
func TestGeminiChatsDump(t *testing.T) {
	p := provider.NewGeminiProvider()
	path := filepath.Join("..", "..", "testdata", "gemini", "chats", "session-test.jsonl")

	conv, err := p.Dump(path, provider.Options{})
	if err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	if conv.ResumeID != "15e2aaaa-bbbb-cccc-dddd-eeeeffff1111" {
		t.Errorf("ResumeID = %q", conv.ResumeID)
	}
	if len(conv.Messages) != 2 {
		t.Fatalf("expected 2 messages from last snapshot, got %d: %+v", len(conv.Messages), conv.Messages)
	}
	if conv.Messages[0].Role != "user" || conv.Messages[0].Content != "first" {
		t.Errorf("message 0 = %+v", conv.Messages[0])
	}
	if conv.Messages[1].Role != "assistant" || conv.Messages[1].Content != "second" {
		t.Errorf("message 1 = %+v", conv.Messages[1])
	}
	if conv.Cwd != "" {
		t.Errorf("Cwd should be empty (last snapshot has no workspace block), got %q", conv.Cwd)
	}
	if conv.UpdatedAt.Format("2006-01-02T15:04:05Z") != "2026-06-22T18:30:00Z" {
		t.Errorf("UpdatedAt should come from header lastUpdated, got %v", conv.UpdatedAt)
	}
}

// TestGeminiChatsWorkspaceCwd verifies Cwd extraction when the workspace block is
// present in the winning (last) snapshot's first user message.
func TestGeminiChatsWorkspaceCwd(t *testing.T) {
	p := provider.NewGeminiProvider()
	path := filepath.Join("..", "..", "testdata", "gemini", "chats_ws", "session-ws.jsonl")

	conv, err := p.Dump(path, provider.Options{})
	if err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	if conv.Cwd != "/home/u/work/infra" {
		t.Errorf("Cwd = %q, want /home/u/work/infra", conv.Cwd)
	}
	if len(conv.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(conv.Messages))
	}
	if !strings.Contains(conv.Messages[0].Content, "Workspace Directories") {
		t.Errorf("first message should retain its raw content, got %q", conv.Messages[0].Content)
	}
}

func TestGeminiLegacyObjectDump(t *testing.T) {
	p := provider.NewGeminiProvider()
	path := filepath.Join("..", "..", "testdata", "gemini", "mock3.json")

	conv, err := p.Dump(path, provider.Options{})
	if err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	if conv.Title != "Gemini brainstorm" {
		t.Errorf("Title = %q", conv.Title)
	}
	if len(conv.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(conv.Messages), conv.Messages)
	}
	if conv.Messages[0].Role != "user" || conv.Messages[0].Content != "List 3 ideas" {
		t.Errorf("message 0 = %+v", conv.Messages[0])
	}
}
