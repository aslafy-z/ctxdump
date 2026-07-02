package provider_test

import (
	"path/filepath"
	"testing"

	"github.com/user/ctxdump/pkg/provider"
)

// TestAntigravityNonStringToolArgs is a regression test: a step whose tool_calls
// carry non-string args (number, bool, nested object) used to fail unmarshal and
// drop the whole step. The step must survive and its Cwd arg must be picked up.
func TestAntigravityNonStringToolArgs(t *testing.T) {
	p := provider.NewAntigravityProvider()
	dir := filepath.Join("..", "..", "testdata", "antigravity", "nonstring-args")

	conv, err := p.Dump("conv-nonstring", provider.Options{CustomPath: dir})
	if err != nil {
		t.Fatalf("Dump failed: %v", err)
	}

	if conv.Cwd != "/proj" {
		t.Errorf("Cwd = %q, want /proj", conv.Cwd)
	}

	foundResp := false
	for _, m := range conv.Messages {
		if m.Role == "assistant" && m.Content == "resp" {
			foundResp = true
		}
	}
	if !foundResp {
		t.Errorf("assistant 'resp' message missing; step with non-string args was dropped: %+v", conv.Messages)
	}
}
