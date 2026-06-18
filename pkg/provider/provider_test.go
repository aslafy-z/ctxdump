package provider_test

import (
	"path/filepath"
	"testing"

	"github.com/user/ctxdump/pkg/provider"
)

func TestProviderList(t *testing.T) {
	reg := provider.NewRegistry()
	codex, _ := reg.Get("codex")

	opts := provider.Options{CustomPath: filepath.Join("..", "..", "testdata", "codex")}
	convs, err := codex.List(opts)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(convs) != 2 {
		t.Errorf("Expected 2 conversations, got %d", len(convs))
	} else {
		foundMock1 := false
		foundMock2 := false
		for _, c := range convs {
			if c.ID == "mock1.json" {
				foundMock1 = true
			}
			if c.ID == "mock2.jsonl" {
				foundMock2 = true
			}
		}
		if !foundMock1 || !foundMock2 {
			t.Errorf("Expected mock1.json and mock2.jsonl, got %v", convs)
		}
	}
}

func TestProviderCodexJSONL(t *testing.T) {
	reg := provider.NewRegistry()
	codex, _ := reg.Get("codex")

	opts := provider.Options{CustomPath: filepath.Join("..", "..", "testdata", "codex")}
	conv, err := codex.Dump("mock2.jsonl", opts)
	if err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	if len(conv.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(conv.Messages))
	}
	if conv.Messages[0].Content != "This is a codex jsonl title fallback test" {
		t.Errorf("Unexpected first message: %s", conv.Messages[0].Content)
	}
}

func TestProviderDump(t *testing.T) {
	reg := provider.NewRegistry()
	claude, _ := reg.Get("claude")

	opts := provider.Options{CustomPath: filepath.Join("..", "..", "testdata", "claude")}
	conv, err := claude.Dump("mock2.json", opts)
	if err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	if conv.Title != "Claude coding session" {
		t.Errorf("Expected title 'Claude coding session', got %q", conv.Title)
	}

	if len(conv.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(conv.Messages))
	}
}
