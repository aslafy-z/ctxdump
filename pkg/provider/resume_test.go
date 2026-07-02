package provider_test

import (
	"reflect"
	"testing"

	"github.com/user/ctxdump/pkg/models"
	"github.com/user/ctxdump/pkg/provider"
)

func asResumer(t *testing.T, p provider.Provider) provider.Resumer {
	t.Helper()
	r, ok := p.(provider.Resumer)
	if !ok {
		t.Fatalf("%s provider does not implement Resumer", p.Name())
	}
	return r
}

func TestCodexResumeSpec(t *testing.T) {
	r := asResumer(t, provider.NewCodexProvider())

	// Explicit ResumeID.
	spec, err := r.ResumeSpec(models.Conversation{ResumeID: "abc", Cwd: "/work"}, provider.Options{}, nil)
	if err != nil {
		t.Fatalf("ResumeSpec failed: %v", err)
	}
	if spec.Command != "codex" {
		t.Errorf("Command = %q", spec.Command)
	}
	if !reflect.DeepEqual(spec.Args, []string{"resume", "abc"}) {
		t.Errorf("Args = %v", spec.Args)
	}
	if spec.Dir != "/work" {
		t.Errorf("Dir = %q", spec.Dir)
	}

	// Rollout filename prefix stripping down to the trailing uuid, plus prompt.
	conv := models.Conversation{
		ID:  "rollout-2026-06-27T11-19-06-019f0860-2413-7282-b259-52d0c9fde043.jsonl",
		Cwd: "/proj",
	}
	spec, err = r.ResumeSpec(conv, provider.Options{}, []string{"hi", "there"})
	if err != nil {
		t.Fatalf("ResumeSpec failed: %v", err)
	}
	want := []string{"resume", "019f0860-2413-7282-b259-52d0c9fde043", "hi", "there"}
	if !reflect.DeepEqual(spec.Args, want) {
		t.Errorf("Args = %v, want %v", spec.Args, want)
	}
	if spec.Dir != "/proj" {
		t.Errorf("Dir = %q", spec.Dir)
	}
}

func TestClaudeResumeSpec(t *testing.T) {
	r := asResumer(t, provider.NewClaudeProvider())

	spec, err := r.ResumeSpec(models.Conversation{ResumeID: "sess-1"}, provider.Options{}, []string{"go"})
	if err != nil {
		t.Fatalf("ResumeSpec failed: %v", err)
	}
	if spec.Command != "claude" {
		t.Errorf("Command = %q", spec.Command)
	}
	if !reflect.DeepEqual(spec.Args, []string{"--resume", "sess-1", "go"}) {
		t.Errorf("Args = %v", spec.Args)
	}

	// Fallback: ResumeID derived from ID with extension stripped.
	spec, err = r.ResumeSpec(models.Conversation{ID: "session-file.jsonl"}, provider.Options{}, nil)
	if err != nil {
		t.Fatalf("ResumeSpec failed: %v", err)
	}
	if !reflect.DeepEqual(spec.Args, []string{"--resume", "session-file"}) {
		t.Errorf("Args = %v", spec.Args)
	}
}

func TestGeminiResumeSpec(t *testing.T) {
	r := asResumer(t, provider.NewGeminiProvider())

	spec, err := r.ResumeSpec(models.Conversation{ResumeID: "gid", Cwd: "/g"}, provider.Options{}, []string{"resume-me"})
	if err != nil {
		t.Fatalf("ResumeSpec failed: %v", err)
	}
	if spec.Command != "gemini" {
		t.Errorf("Command = %q", spec.Command)
	}
	if !reflect.DeepEqual(spec.Args, []string{"resume", "gid", "resume-me"}) {
		t.Errorf("Args = %v", spec.Args)
	}
	if spec.Dir != "/g" {
		t.Errorf("Dir = %q", spec.Dir)
	}
}
