package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/ctxdump/pkg/models"
)

type claudeProvider struct{}

func NewClaudeProvider() Provider {
	return &claudeProvider{}
}

func (p *claudeProvider) Name() string {
	return "claude"
}

func (p *claudeProvider) List(opts Options) ([]models.Conversation, error) {
	paths := p.getPaths(opts)
	var conversations []models.Conversation

	for _, dir := range paths {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				// Skip plugin bundles and subagent (sidechain) transcripts, which
				// live under a `subagents/` directory and are not standalone
				// conversations.
				if d.Name() == "plugins" || d.Name() == "subagents" {
					return fs.SkipDir
				}
				return nil
			}
			name := d.Name()
			if name == "settings.json" || name == "settings.local.json" || name == "stats-cache.json" || name == "sessions-index.json" || name == "plugin.json" || name == ".mcp.json" {
				return nil
			}
			title, snippet, cwd, resumeID, isSidechain := extractClaudeMeta(path)

			// Subagent (sidechain) transcripts are not standalone conversations.
			// They usually live under a `subagents/` directory (skipped above), but
			// exclude any that are recorded inline as well.
			if isSidechain {
				return nil
			}

			// Fallback: if not found in file and looks like an encoded path.
			if cwd == "" && strings.Contains(path, ".claude/projects/") {
				dir := filepath.Dir(path)
				base := filepath.Base(dir)
				// The encoding replaces path separators with hyphens, so this naive
				// decode is lossy for directories that contain literal hyphens. It is
				// kept as a best-effort fallback for legacy files missing `cwd`;
				// faithful reconstruction is a follow-up.
				cwd = strings.ReplaceAll(base, "-", "/")
			}

			if strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".jsonl") {
				info, err := d.Info()
				if err != nil {
					return nil
				}
				if title == "" && snippet != "" {
					title = snippet
					if len(title) > 50 {
						title = models.TruncateRunes(title, 47) + "..."
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

func extractClaudeMeta(path string) (title, firstMessage, cwd, resumeID string, isSidechain bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", "", "", false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	// Increase buffer for very large lines (like attachments)
	scanner.Buffer(make([]byte, 0, 256*1024), 2*1024*1024)

	for i := 0; i < 200 && scanner.Scan(); i++ {
		line := scanner.Text()

		if !isSidechain && strings.Contains(line, `"isSidechain":true`) {
			isSidechain = true
		}

		// Fast-path json parsing for lines containing known keys to avoid unmarshaling everything
		needsParse := false
		if title == "" && strings.Contains(line, `"aiTitle":`) {
			needsParse = true
		}
		if cwd == "" && strings.Contains(line, `"cwd":`) {
			needsParse = true
		}
		if firstMessage == "" && strings.Contains(line, `"role":"user"`) {
			needsParse = true
		}
		if resumeID == "" && (strings.Contains(line, `"sessionId":`) || strings.Contains(line, `"session_id":`)) {
			needsParse = true
		}

		if needsParse {
			var obj map[string]interface{}
			if json.Unmarshal([]byte(line), &obj) == nil {
				if title == "" {
					if t, ok := obj["aiTitle"].(string); ok {
						title = t
					} else if t, ok := obj["title"].(string); ok {
						title = t
					}
				}

				if cwd == "" {
					if c, ok := obj["cwd"].(string); ok {
						cwd = c
					}
				}

				if resumeID == "" {
					if id, ok := obj["sessionId"].(string); ok {
						resumeID = id
					} else if id, ok := obj["session_id"].(string); ok {
						resumeID = id
					}
				}

				if firstMessage == "" {
					// Check "message" object
					if msgObj, ok := obj["message"].(map[string]interface{}); ok {
						if r, ok := msgObj["role"].(string); ok && r == "user" {
							if content, ok := msgObj["content"].(string); ok {
								firstMessage = content
							} else if contentArr, ok := msgObj["content"].([]interface{}); ok && len(contentArr) > 0 {
								if firstElem, ok := contentArr[0].(map[string]interface{}); ok {
									if text, ok := firstElem["text"].(string); ok {
										firstMessage = text
									}
								}
							}
						}
					}
				}
			}
		}
	}

	if firstMessage != "" {
		if strings.HasPrefix(firstMessage, "<local-command-caveat>") {
			endIdx := strings.Index(firstMessage, "</local-command-caveat>")
			if endIdx != -1 {
				firstMessage = strings.TrimSpace(firstMessage[endIdx+len("</local-command-caveat>"):])
			} else {
				firstMessage = ""
			}
		}

		firstMessage = strings.ReplaceAll(firstMessage, "\n", " ")
		if len(firstMessage) > 100 {
			firstMessage = models.TruncateRunes(firstMessage, 97) + "..."
		}
	}

	return title, firstMessage, cwd, resumeID, isSidechain
}

func (p *claudeProvider) Dump(idOrFile string, opts Options) (models.Conversation, error) {
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

	// Fallback: search across all discovered conversations
	convs, err := p.List(opts)
	if err == nil {
		for _, c := range convs {
			if c.ID == idOrFile || strings.TrimSuffix(c.ID, ".jsonl") == idOrFile || strings.TrimSuffix(c.ID, ".json") == idOrFile {
				return p.parseFile(c.FilePath)
			}
		}
	}

	return models.Conversation{}, os.ErrNotExist
}

func (p *claudeProvider) getPaths(opts Options) []string {
	if opts.CustomPath != "" {
		return []string{opts.CustomPath}
	}
	var dirs []string
	if home, _ := os.UserHomeDir(); home != "" {
		dirs = append(dirs, filepath.Join(home, ".claude", "projects"))
		dirs = append(dirs, filepath.Join(home, ".claude", "tasks"))
	}
	return dirs
}

func (p *claudeProvider) parseFile(path string) (models.Conversation, error) {
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

	// A whole-file object with an explicit messages array is the legacy shape;
	// anything else is JSONL, including single-line session files that a naive
	// line-count check would misclassify as legacy.
	var whole map[string]interface{}
	if json.Unmarshal(data, &whole) == nil {
		if _, ok := whole["messages"].([]interface{}); ok {
			parseClaudeLegacy(&conv, whole)
			return conv, nil
		}
	}

	var newest time.Time
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]interface{}
		if json.Unmarshal([]byte(line), &obj) != nil {
			continue
		}
		ts := parseRFC3339(obj["timestamp"])
		if ts.After(newest) {
			newest = ts
		}
		parseClaudeLine(&conv, obj, ts)
	}
	if !newest.IsZero() {
		conv.UpdatedAt = newest
	}

	return conv, nil
}

// parseClaudeLine interprets one JSONL record, updating conversation metadata and
// appending any resulting message with its timestamp.
func parseClaudeLine(conv *models.Conversation, obj map[string]interface{}, ts time.Time) {
	if conv.ResumeID == "" {
		if id, ok := obj["sessionId"].(string); ok {
			conv.ResumeID = id
		} else if id, ok := obj["session_id"].(string); ok {
			conv.ResumeID = id
		}
	}
	if conv.Cwd == "" {
		if cwd, ok := obj["cwd"].(string); ok {
			conv.Cwd = cwd
		}
	}
	if conv.Title == "" {
		if t, ok := obj["aiTitle"].(string); ok {
			conv.Title = t
		} else if t, ok := obj["title"].(string); ok {
			conv.Title = t
		}
	}

	if msgObj, ok := obj["message"].(map[string]interface{}); ok {
		role, _ := msgObj["role"].(string)
		var contentStr string
		isThought := false

		if content, ok := msgObj["content"].(string); ok {
			contentStr = content
		} else if contentArr, ok := msgObj["content"].([]interface{}); ok {
			for _, cElem := range contentArr {
				cMap, ok := cElem.(map[string]interface{})
				if !ok {
					continue
				}
				switch cType, _ := cMap["type"].(string); cType {
				case "thinking":
					if t, ok := cMap["thinking"].(string); ok {
						contentStr += t
						isThought = true
					}
				case "tool_use":
					contentStr += renderToolUse(cMap)
				case "tool_result":
					contentStr += renderToolResult(cMap)
				default:
					if t, ok := cMap["text"].(string); ok {
						contentStr += t
					}
				}
			}
		}
		if contentStr != "" {
			conv.Messages = append(conv.Messages, models.Message{Role: role, Content: contentStr, IsThought: isThought, Timestamp: ts})
		}
		return
	}

	switch typ, _ := obj["type"].(string); typ {
	case "system":
		if content, ok := obj["content"].(string); ok && content != "" {
			conv.Messages = append(conv.Messages, models.Message{Role: "system", Content: content, Timestamp: ts})
		} else if contentMap, ok := obj["content"].(map[string]interface{}); ok {
			if text, ok := contentMap["text"].(string); ok && text != "" {
				conv.Messages = append(conv.Messages, models.Message{Role: "system", Content: text, Timestamp: ts})
			}
		}
	case "attachment":
		if att, ok := obj["attachment"].(map[string]interface{}); ok {
			if stdout, ok := att["stdout"].(string); ok && stdout != "" {
				conv.Messages = append(conv.Messages, models.Message{Role: "tool", Content: stdout, Timestamp: ts})
			}
		}
	}
}

// parseClaudeLegacy interprets the legacy whole-file object shape.
func parseClaudeLegacy(conv *models.Conversation, obj map[string]interface{}) {
	if id, ok := obj["sessionId"].(string); ok {
		conv.ResumeID = id
	} else if id, ok := obj["session_id"].(string); ok {
		conv.ResumeID = id
	}
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
		var contentStr string
		if content, ok := mmap["content"].(string); ok {
			contentStr = content
		} else if contentArr, ok := mmap["content"].([]interface{}); ok {
			for _, cElem := range contentArr {
				if cMap, ok := cElem.(map[string]interface{}); ok {
					if t, ok := cMap["text"].(string); ok {
						contentStr += t
					}
				}
			}
		}
		if contentStr != "" {
			conv.Messages = append(conv.Messages, models.Message{Role: role, Content: contentStr})
		}
	}
}

// renderToolUse turns a tool_use content block into a readable representation,
// preserving the meaningful input (command, file content, edit diff) instead of
// collapsing it to the tool name.
func renderToolUse(block map[string]interface{}) string {
	name, _ := block["name"].(string)
	input, _ := block["input"].(map[string]interface{})

	if input == nil {
		return fmt.Sprintf("[Tool Use: %s]", name)
	}

	str := func(key string) string {
		s, _ := input[key].(string)
		return s
	}

	switch name {
	case "Bash":
		var sb strings.Builder
		sb.WriteString("[Tool Use: Bash]\n")
		if desc := str("description"); desc != "" {
			sb.WriteString("# " + desc + "\n")
		}
		sb.WriteString("$ " + str("command"))
		return sb.String()
	case "Write":
		return fmt.Sprintf("[Tool Use: Write %s]\n%s", str("file_path"), str("content"))
	case "Edit":
		return fmt.Sprintf("[Tool Use: Edit %s]\n%s",
			str("file_path"), UnifiedDiff(str("file_path"), str("old_string"), str("new_string")))
	case "Read":
		return fmt.Sprintf("[Tool Use: Read %s]", str("file_path"))
	default:
		if b, err := json.Marshal(input); err == nil {
			return fmt.Sprintf("[Tool Use: %s] %s", name, string(b))
		}
		return fmt.Sprintf("[Tool Use: %s]", name)
	}
}

// renderToolResult extracts the textual output of a tool_result content block,
// whose content may be a plain string or an array of text blocks.
func renderToolResult(block map[string]interface{}) string {
	switch c := block["content"].(type) {
	case string:
		return c
	case []interface{}:
		var sb strings.Builder
		for _, elem := range c {
			if m, ok := elem.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
		return sb.String()
	default:
		return ""
	}
}

func (p *claudeProvider) ResumeSpec(
	conv models.Conversation,
	opts Options,
	prompt []string,
) (ResumeSpec, error) {
	resumeID := conv.ResumeID
	if resumeID == "" {
		resumeID = strings.TrimSuffix(conv.ID, ".jsonl")
		resumeID = strings.TrimSuffix(resumeID, ".json")
	}

	args := []string{"--resume"}
	if resumeID != "" {
		args = append(args, resumeID)
	}
	args = append(args, prompt...)

	dir := conv.Cwd
	if dir == "" {
		dir = "" // Let execution use the current working directory
	}

	return ResumeSpec{
		Command: "claude",
		Args:    args,
		Dir:     dir,
	}, nil
}
