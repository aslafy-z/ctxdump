package provider

import (
	"strings"

	"github.com/user/ctxdump/pkg/models"
)

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
