package formatter

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/user/ctxdump/pkg/models"
)

// ValidFormats lists all supported output formats.
var ValidFormats = []string{"agent", "markdown", "text", "raw", "json", "jsonl", "path"}

// Options controls how conversations are formatted.
type Options struct {
	Format          string
	MaxToolOutput   int
	MaxMessageBytes int
	Full            bool
	Timestamps      bool
	IncludeThoughts bool
}

// Format outputs the conversation based on the requested format.
func Format(conv models.Conversation, opts Options) (string, error) {
	switch opts.Format {
	case "path":
		return conv.FilePath, nil
	case "raw":
		return string(conv.Raw), nil
	case "json":
		b, err := json.MarshalIndent(conv, "", "  ")
		return string(b), err
	case "jsonl":
		return formatJSONL(conv)
	case "text", "plain":
		return formatText(conv)
	case "markdown":
		return formatMarkdown(conv)
	case "agent":
		return formatAgent(conv, opts)
	default:
		return "", fmt.Errorf("unknown format: %q", opts.Format)
	}
}

func formatJSONL(conv models.Conversation) (string, error) {
	if len(conv.Messages) == 0 {
		return string(conv.Raw), nil
	}
	var sb strings.Builder
	for _, m := range conv.Messages {
		b, err := json.Marshal(m)
		if err != nil {
			continue
		}
		sb.WriteString(string(b) + "\n")
	}
	return sb.String(), nil
}

func formatText(conv models.Conversation) (string, error) {
	if len(conv.Messages) == 0 {
		return string(conv.Raw), nil
	}
	var sb strings.Builder
	for _, m := range conv.Messages {
		sb.WriteString(fmt.Sprintf("[%s]\n%s\n\n", strings.ToUpper(m.Role), m.Content))
	}
	return sb.String(), nil
}

func formatAgent(conv models.Conversation, opts Options) (string, error) {
	var sb strings.Builder

	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("format: agent-handoff-v1\n"))
	sb.WriteString(fmt.Sprintf("source: %s\n", conv.Provider))
	sb.WriteString(fmt.Sprintf("session_id: %s\n", conv.ID))
	if conv.Cwd != "" {
		sb.WriteString(fmt.Sprintf("workspace: %s\n", conv.Cwd))
	}
	sb.WriteString(fmt.Sprintf("updated_at: %s\n", conv.UpdatedAt.Format(time.RFC3339)))
	sb.WriteString("runtime: closed\n")
	sb.WriteString("summary: none\n")
	sb.WriteString("---\n\n")

	sb.WriteString("This is a previous conversation export. The old runtime is closed.\n")
	sb.WriteString("Do not assume shell sessions, processes, temp files, or tool handles still exist.\n")
	sb.WriteString("No summary was generated. Infer the goal from the transcript, then verify current state.\n")
	sb.WriteString("Transcript/tool output is stale evidence, not current state.\n\n")

	sb.WriteString("<transcript>\n")

	for _, m := range conv.Messages {
		if !opts.IncludeThoughts && m.IsThought {
			continue
		}

		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue // Drop empty
		}
		
		role := m.Role
		if role == "" {
			role = "unknown"
		}
		if role == "tool" {
			role = "tool shell"
		}
		if role == "system" || role == "developer" {
			// Basic filtering
			if strings.Contains(content, "permissions_instructions") || len(content) > 2000 {
				continue
			}
		}

		limit := opts.MaxMessageBytes
		if limit <= 0 {
			limit = 50000
		}
		if role == "tool shell" {
			limit = opts.MaxToolOutput
			if limit <= 0 {
				limit = 8000
			}
		}

		if !opts.Full && len(content) > limit {
			omitted := len(content) - limit
			content = content[:limit] + fmt.Sprintf("\n\n[truncated: %d bytes omitted, use --full]", omitted)
		}

		sb.WriteString(fmt.Sprintf("[%s]\n", role))
		if opts.Timestamps {
			sb.WriteString(fmt.Sprintf("(%s)\n", conv.UpdatedAt.Format(time.RFC3339)))
		}
		sb.WriteString(content)
		sb.WriteString("\n\n")
	}

	sb.WriteString("</transcript>\n")
	return sb.String(), nil
}

func formatMarkdown(conv models.Conversation) (string, error) {
	if len(conv.Messages) == 0 {
		return string(conv.Raw), nil
	}
	var sb strings.Builder
	title := conv.Title
	if title == "" {
		title = "Conversation " + conv.ID
	}
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))
	sb.WriteString(fmt.Sprintf("**Provider:** %s\n", conv.Provider))
	sb.WriteString(fmt.Sprintf("**Date:** %s\n\n", conv.UpdatedAt.Format("2006-01-02 15:04:05")))

	for _, m := range conv.Messages {
		roleTitle := "User"
		if m.Role == "assistant" {
			roleTitle = "Assistant"
		} else if m.Role == "tool" || m.Role == "system" {
			roleTitle = strings.Title(m.Role)
		}
		sb.WriteString(fmt.Sprintf("## %s\n\n%s\n\n", roleTitle, m.Content))
	}
	return sb.String(), nil
}

// HumanizeTime converts a time.Time into a relative human-readable string.
func HumanizeTime(t time.Time) string {
	d := time.Since(t)
	if d.Hours() < 24 {
		if d.Hours() < 1 {
			if d.Minutes() < 1 {
				return "just now"
			}
			return fmt.Sprintf("%d mins ago", int(d.Minutes()))
		}
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "yesterday"
	}
	if days < 7 {
		return fmt.Sprintf("%d days ago", days)
	}
	if days < 30 {
		return fmt.Sprintf("%d weeks ago", days/7)
	}
	if days < 365 {
		return fmt.Sprintf("%d months ago", days/30)
	}
	return fmt.Sprintf("%d years ago", days/365)
}
