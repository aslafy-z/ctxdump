package provider

import (
	"fmt"
	"strings"
	"time"

	"github.com/user/ctxdump/pkg/models"
)

// joinTextBlocks concatenates the text of a content field that may be a plain
// string or an array of `{..., "text": ...}` blocks, as used by codex and gemini
// records.
func joinTextBlocks(v interface{}) string {
	switch c := v.(type) {
	case string:
		return c
	case []interface{}:
		var sb strings.Builder
		for _, e := range c {
			if m, ok := e.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
		return sb.String()
	}
	return ""
}

// parseRFC3339 parses an RFC3339 timestamp from an untyped value, returning the
// zero time on any failure.
func parseRFC3339(v interface{}) time.Time {
	s, ok := v.(string)
	if !ok || s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// UnifiedDiff renders a before/after pair as a unified diff for the given file,
// so edits are presented consistently across providers.
func UnifiedDiff(file, oldStr, newStr string) string {
	var sb strings.Builder
	if file != "" {
		fmt.Fprintf(&sb, "--- %s\n+++ %s\n", file, file)
	}
	sb.WriteString("@@ @@\n")
	for _, line := range strings.Split(oldStr, "\n") {
		sb.WriteString("-" + line + "\n")
	}
	for _, line := range strings.Split(newStr, "\n") {
		sb.WriteString("+" + line + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// Options holds common configuration for providers.
type Options struct {
	CustomPath string
}

// Provider defines the interface for discovering and reading local AI conversations.
type Provider interface {
	// Name returns the provider's identifier (e.g., "codex", "claude", "gemini").
	Name() string

	// List discovers conversations managed by this provider.
	List(opts Options) ([]models.Conversation, error)

	// Dump reads a specific conversation by its ID or filepath.
	Dump(idOrFile string, opts Options) (models.Conversation, error)
}

// ResumeSpec defines the command to execute for resuming a conversation natively.
type ResumeSpec struct {
	Command string
	Args    []string
	Dir     string
	Env     []string
}

// Resumer is an optional interface that providers can implement to support native resume.
type Resumer interface {
	ResumeSpec(conv models.Conversation, opts Options, prompt []string) (ResumeSpec, error)
}

// StripSystemTags removes common noisy system-injected XML blocks.
func StripSystemTags(content string) string {
	tags := []string{
		"permissions instructions",
		"plugins_instructions",
		"environment_context",
		"local-command-caveat",
		"USER_REQUEST",
		"ADDITIONAL_METADATA",
		"USER_SETTINGS_CHANGE",
		"SYSTEM_MESSAGE",
		"EPHEMERAL_MESSAGE",
	}
	for _, tag := range tags {
		startTag := "<" + tag + ">"
		endTag := "</" + tag + ">"
		for {
			start := strings.Index(content, startTag)
			if start == -1 {
				break
			}
			end := strings.Index(content, endTag)
			if end == -1 || end < start {
				// If unclosed or malformed, just stop processing this tag
				break
			}
			content = content[:start] + content[end+len(endTag):]
		}
	}
	return strings.TrimSpace(content)
}
