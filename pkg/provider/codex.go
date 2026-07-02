package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/ctxdump/pkg/models"
)

type codexProvider struct{}

func NewCodexProvider() Provider {
	return &codexProvider{}
}

func (p *codexProvider) Name() string {
	return "codex"
}

func (p *codexProvider) List(opts Options) ([]models.Conversation, error) {
	paths := p.getPaths(opts)
	var conversations []models.Conversation

	for _, dir := range paths {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if strings.HasSuffix(d.Name(), ".json") || strings.HasSuffix(d.Name(), ".jsonl") {
				info, err := d.Info()
				if err != nil {
					return nil
				}
				title := ""
				snippet := ""
				cwd := ""
				resumeID := ""
				if c, err := p.parseFile(path); err == nil {
					title = c.Title
					cwd = c.Cwd
					resumeID = c.ResumeID
					for _, m := range c.Messages {
						content := strings.TrimSpace(m.Content)
						if m.Role == "user" {
							if content != "" {
								snippet = content
								break
							}
						}
					}
					if snippet == "" && len(c.Messages) > 0 {
						snippet = c.Messages[0].Content
					}

					snippet = strings.ReplaceAll(snippet, "\n", " ")
					if len(snippet) > 100 {
						snippet = models.TruncateRunes(snippet, 97) + "..."
					}

					if title == "" {
						title = snippet
						if len(title) > 50 {
							title = models.TruncateRunes(title, 47) + "..."
						}
					}
				}
				conversations = append(conversations, models.Conversation{
					ID:        d.Name(),
					Provider:  p.Name(),
					FilePath:  path,
					Title:     title,
					Snippet:   snippet,
					Cwd:       cwd,
					ResumeID:  resumeID,
					UpdatedAt: info.ModTime(),
				})
			}
			return nil
		})
		if err != nil {
			continue
		}
	}
	return conversations, nil
}

func (p *codexProvider) Dump(idOrFile string, opts Options) (models.Conversation, error) {
	if _, err := os.Stat(idOrFile); err == nil {
		return p.parseFile(idOrFile)
	}
	paths := p.getPaths(opts)
	for _, dir := range paths {
		target := filepath.Join(dir, idOrFile)
		if _, err := os.Stat(target); err == nil {
			return p.parseFile(target)
		}
	}
	return models.Conversation{}, os.ErrNotExist
}

func (p *codexProvider) getPaths(opts Options) []string {
	if opts.CustomPath != "" {
		return []string{opts.CustomPath}
	}
	var dirs []string
	if home, _ := os.UserHomeDir(); home != "" {
		dirs = append(dirs, filepath.Join(home, ".codex", "sessions"))
	}
	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		dirs = append(dirs, filepath.Join(codexHome, "sessions"))
	}
	return dirs
}

func (p *codexProvider) parseFile(path string) (models.Conversation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return models.Conversation{}, err
	}

	conv := models.Conversation{
		ID:       filepath.Base(path),
		Provider: p.Name(),
		FilePath: path,
		Raw:      data,
	}
	if info, err := os.Stat(path); err == nil {
		conv.UpdatedAt = info.ModTime()
	}

	// Whole-file JSON object carrying an explicit messages array (simple shape).
	var obj map[string]interface{}
	if json.Unmarshal(data, &obj) == nil {
		if _, ok := obj["messages"].([]interface{}); ok {
			parseCodexObject(&conv, obj)
			return conv, nil
		}
	}

	// JSONL rollout: one record per line. Grow the buffer well past bufio's 64KB
	// default so a single long tool-output line cannot truncate the rest of the file.
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	seen := make(map[string]bool)
	var newest time.Time
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec map[string]interface{}
		if json.Unmarshal(line, &rec) != nil {
			continue
		}

		ts := parseRFC3339(rec["timestamp"])
		if ts.After(newest) {
			newest = ts
		}
		if title, ok := rec["title"].(string); ok && conv.Title == "" {
			conv.Title = title
		}

		recType, _ := rec["type"].(string)
		if pl, ok := rec["payload"].(map[string]interface{}); ok {
			switch recType {
			case "session_meta":
				if conv.ResumeID == "" {
					conv.ResumeID = codexID(pl)
				}
				if cwd, ok := pl["cwd"].(string); ok && cwd != "" {
					conv.Cwd = cwd
				}
			case "turn_context":
				if conv.Cwd == "" {
					if cwd, ok := pl["cwd"].(string); ok {
						conv.Cwd = cwd
					}
				}
			default:
				applyCodexPayload(&conv, pl, ts, seen)
			}
		} else {
			// Pre-payload (2025-era) records: the record itself is the payload.
			if conv.ResumeID == "" {
				conv.ResumeID = codexID(rec)
			}
			applyCodexPayload(&conv, rec, ts, seen)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: codex: error reading %s: %v\n", path, err)
	}
	if !newest.IsZero() {
		conv.UpdatedAt = newest
	}

	return conv, nil
}

// parseCodexObject handles the whole-file JSON object shape (a `messages` array
// of `{role, content}` string entries).
func parseCodexObject(conv *models.Conversation, obj map[string]interface{}) {
	conv.ResumeID = codexID(obj)
	if title, ok := obj["title"].(string); ok {
		conv.Title = title
	}
	msgs, _ := obj["messages"].([]interface{})
	for _, m := range msgs {
		mmap, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := mmap["role"].(string)
		content, _ := mmap["content"].(string)
		if content == "" {
			continue
		}
		if c := extractCwdTag(content); c != "" {
			conv.Cwd = c
		}
		content = StripSystemTags(content)
		if content != "" {
			conv.Messages = append(conv.Messages, models.Message{Role: role, Content: content})
		}
	}
}

// applyCodexPayload interprets a single codex record payload (or, for pre-payload
// rollouts, the record itself) and appends any resulting transcript message.
// Conversational user/assistant text is deduplicated via seen so that the twin
// `response_item/message` and `event_msg/*_message` copies collapse to one entry.
func applyCodexPayload(conv *models.Conversation, pl map[string]interface{}, ts time.Time, seen map[string]bool) {
	// Tool activity (commands, patches, MCP calls) carries no conversational text.
	if tool := renderCodexTool(pl); tool != "" {
		conv.Messages = append(conv.Messages, models.Message{Role: "tool", Content: tool, Timestamp: ts})
		return
	}

	plType, _ := pl["type"].(string)
	switch plType {
	case "reasoning":
		if text := joinCodexSummary(pl["summary"]); text != "" {
			conv.Messages = append(conv.Messages, models.Message{
				Role: "assistant", Content: text, IsThought: true, Timestamp: ts,
			})
		}
	case "user_message":
		if msg, ok := pl["message"].(string); ok {
			appendCodexDedup(conv, seen, "user", msg, false, ts)
		}
	case "agent_message":
		if msg, ok := pl["message"].(string); ok {
			appendCodexDedup(conv, seen, "assistant", msg, false, ts)
		}
	case "message", "":
		role, _ := pl["role"].(string)
		content := joinTextBlocks(pl["content"])
		if content == "" {
			return
		}
		if c := extractCwdTag(content); c != "" && conv.Cwd == "" {
			conv.Cwd = c
		}
		isThought := false
		if role == "thought" || role == "thinking" {
			isThought = true
			role = "assistant"
		} else if role == "" {
			// Empty role with content in codex is almost always commentary/thought.
			isThought = true
		}
		appendCodexDedup(conv, seen, role, content, isThought, ts)
	}
}

// appendCodexDedup strips system noise, drops empties, and skips a message whose
// content has already been appended (the response_item / event_msg duplication).
func appendCodexDedup(conv *models.Conversation, seen map[string]bool, role, content string, isThought bool, ts time.Time) {
	content = StripSystemTags(content)
	if content == "" {
		return
	}
	key := strings.TrimSpace(content)
	if seen[key] {
		return
	}
	seen[key] = true
	conv.Messages = append(conv.Messages, models.Message{Role: role, Content: content, IsThought: isThought, Timestamp: ts})
}

// codexID returns the first present session identifier from a record/payload.
func codexID(m map[string]interface{}) string {
	for _, k := range []string{"id", "session_id", "sessionId"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// joinCodexSummary joins the text of a reasoning payload's summary blocks.
func joinCodexSummary(v interface{}) string {
	arr, ok := v.([]interface{})
	if !ok {
		return ""
	}
	var parts []string
	for _, e := range arr {
		if m, ok := e.(map[string]interface{}); ok {
			if t, ok := m["text"].(string); ok && t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// extractCwdTag returns the value inside a `<cwd>...</cwd>` marker, or "".
func extractCwdTag(content string) string {
	start := strings.Index(content, "<cwd>")
	end := strings.Index(content, "</cwd>")
	if start != -1 && end != -1 && start+5 < end {
		return content[start+5 : end]
	}
	return ""
}

// renderCodexTool turns a codex tool-activity payload (command execution, patch
// application, MCP/custom tool call) into a readable transcript entry. It returns
// an empty string for payload types that are not tool activity.
func renderCodexTool(payload map[string]interface{}) string {
	typ, _ := payload["type"].(string)
	str := func(key string) string {
		s, _ := payload[key].(string)
		return s
	}

	switch typ {
	case "function_call":
		name := str("name")
		args := str("arguments")
		if name == "exec_command" {
			if cmd := extractJSONField(args, "cmd"); cmd != "" {
				return "$ " + cmd
			}
		}
		if args != "" {
			return fmt.Sprintf("[Tool Call: %s] %s", name, args)
		}
		return fmt.Sprintf("[Tool Call: %s]", name)

	case "function_call_output":
		if out := str("output"); out != "" {
			return out
		}

	case "custom_tool_call":
		name := str("name")
		input := str("input")
		if name == "apply_patch" {
			return "[Tool Call: apply_patch]\n" + input
		}
		if input != "" {
			return fmt.Sprintf("[Tool Call: %s] %s", name, input)
		}
		return fmt.Sprintf("[Tool Call: %s]", name)

	case "custom_tool_call_output":
		if out := str("output"); out != "" {
			return out
		}

	case "patch_apply_end":
		var sb strings.Builder
		if out := str("stdout"); out != "" {
			sb.WriteString(out)
		}
		if errOut := str("stderr"); errOut != "" {
			sb.WriteString(errOut)
		}
		return strings.TrimSpace(sb.String())

	case "exec_command_end":
		if out := str("stdout"); out != "" {
			return out
		}

	case "mcp_tool_call_end":
		if inv, ok := payload["invocation"].(map[string]interface{}); ok {
			server, _ := inv["server"].(string)
			tool, _ := inv["tool"].(string)
			return fmt.Sprintf("[MCP Call: %s/%s]", server, tool)
		}
	}

	return ""
}

// extractJSONField unmarshals a JSON object string and returns the named string field.
func extractJSONField(jsonStr, field string) string {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		return ""
	}
	s, _ := obj[field].(string)
	return s
}

func (p *codexProvider) ResumeSpec(
	conv models.Conversation,
	opts Options,
	prompt []string,
) (ResumeSpec, error) {
	resumeID := conv.ResumeID
	if resumeID == "" {
		resumeID = strings.TrimSuffix(conv.ID, ".jsonl")
		resumeID = strings.TrimSuffix(resumeID, ".json")
	}

	if strings.HasPrefix(resumeID, "rollout-") && len(resumeID) >= 36 {
		resumeID = resumeID[len(resumeID)-36:]
	}

	args := []string{"resume"}
	if resumeID != "" {
		args = append(args, resumeID)
	}
	args = append(args, prompt...)

	dir := conv.Cwd
	if dir == "" {
		dir = "" // Let execution use the current working directory
	}

	return ResumeSpec{
		Command: "codex",
		Args:    args,
		Dir:     dir,
	}, nil
}
