package provider_test

import (
	"path/filepath"
	"testing"

	"github.com/user/ctxdump/pkg/provider"
)

func TestAntigravityList(t *testing.T) {
	reg := provider.NewRegistry()
	ag, err := reg.Get("antigravity")
	if err != nil {
		t.Fatalf("Could not get antigravity provider: %v", err)
	}

	opts := provider.Options{CustomPath: filepath.Join("..", "..", "testdata", "antigravity")}
	convs, err := ag.List(opts)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(convs) != 1 {
		t.Fatalf("Expected 1 conversation, got %d", len(convs))
	}

	c := convs[0]
	if c.ID != "test-conv-001" {
		t.Errorf("Expected ID 'test-conv-001', got %q", c.ID)
	}
	if c.Title != "Fix the bug in the login handler" {
		t.Errorf("Expected Title 'Fix the bug in the login handler', got %q", c.Title)
	}
	if c.Cwd != "/home/testuser/work/myproject/auth" {
		t.Errorf("Expected Cwd '/home/testuser/work/myproject/auth', got %q", c.Cwd)
	}
}

func TestAntigravityDump(t *testing.T) {
	reg := provider.NewRegistry()
	ag, err := reg.Get("antigravity")
	if err != nil {
		t.Fatalf("Could not get antigravity provider: %v", err)
	}

	opts := provider.Options{CustomPath: filepath.Join("..", "..", "testdata", "antigravity")}
	conv, err := ag.Dump("test-conv-001", opts)
	if err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	if len(conv.Messages) != 8 {
		t.Fatalf("Expected 8 messages, got %d", len(conv.Messages))
	}

	if conv.Messages[0].Role != "user" || conv.Messages[0].Content != "Fix the bug in the login handler" {
		t.Errorf("Message 0 mismatch: %+v", conv.Messages[0])
	}
	if conv.Messages[1].Role != "assistant" || !conv.Messages[1].IsThought {
		t.Errorf("Message 1 should be a thought: %+v", conv.Messages[1])
	}
	if conv.Messages[2].Role != "assistant" || conv.Messages[2].Content != "Let me look at the login handler to identify the bug." {
		t.Errorf("Message 2 mismatch: %+v", conv.Messages[2])
	}
	if conv.Messages[3].Role != "tool" || conv.Messages[3].Content == "" {
		t.Errorf("Message 3 should be a tool message: %+v", conv.Messages[3])
	}
	if conv.Messages[4].Role != "assistant" || conv.Messages[4].Content == "" {
		t.Errorf("Message 4 should be final response: %+v", conv.Messages[4])
	}
}
