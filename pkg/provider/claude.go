package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
				if d.Name() == "plugins" {
					return fs.SkipDir
				}
				return nil
			}
			name := d.Name()
			if name == "settings.json" || name == "settings.local.json" || name == "stats-cache.json" || name == "sessions-index.json" || name == "plugin.json" || name == ".mcp.json" {
				return nil
			}
			title, snippet, cwd, resumeID := extractClaudeMeta(path)

			// Fallback: if not found in file and looks like an encoded path
			if cwd == "" && strings.Contains(path, ".claude/projects/") {
				dir := filepath.Dir(path)
				base := filepath.Base(dir)
				// Note: this naive decoding replaces all hyphens, which breaks paths with natural hyphens.
				// However, it's better than nothing for legacy files missing the `cwd` field.
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
						title = title[:47] + "..."
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

func extractClaudeMeta(path string) (string, string, string, string) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", "", ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	// Increase buffer for very large lines (like attachments)
	scanner.Buffer(make([]byte, 0, 256*1024), 2*1024*1024)

	var title string
	var firstMessage string
	var cwd string
	var resumeID string

	for i := 0; i < 200 && scanner.Scan(); i++ {
		line := scanner.Text()

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
			firstMessage = firstMessage[:97] + "..."
		}
	}

	return title, firstMessage, cwd, resumeID
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
	info, err := os.Stat(path)
	modTime := info.ModTime()

	conv := models.Conversation{
		ID:        filepath.Base(path),
		Provider:  p.Name(),
		FilePath:  path,
		UpdatedAt: modTime,
		Raw:       data,
	}

	// Check if it's JSONL (multiple lines of JSON objects)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > 1 && strings.HasPrefix(lines[0], "{") && strings.HasPrefix(lines[1], "{") {
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(line), &obj); err == nil {
				// Extract ResumeID
				if conv.ResumeID == "" {
					if id, ok := obj["sessionId"].(string); ok {
						conv.ResumeID = id
					} else if id, ok := obj["session_id"].(string); ok {
						conv.ResumeID = id
					}
				}

				// Extract Cwd
				if conv.Cwd == "" {
					if cwd, ok := obj["cwd"].(string); ok {
						conv.Cwd = cwd
					}
				}

				// Try to extract title if available
				if conv.Title == "" {
					if t, ok := obj["aiTitle"].(string); ok {
						conv.Title = t
					} else if t, ok := obj["title"].(string); ok {
						conv.Title = t
					}
				}

				if msgObj, ok := obj["message"].(map[string]interface{}); ok {
					role, _ := msgObj["role"].(string)

					// Handle content
					var contentStr string
					isThought := false

					if content, ok := msgObj["content"].(string); ok {
						contentStr = content
					} else if contentArr, ok := msgObj["content"].([]interface{}); ok {
						for _, cElem := range contentArr {
							if cMap, ok := cElem.(map[string]interface{}); ok {
								if cType, _ := cMap["type"].(string); cType == "thinking" {
									if t, ok := cMap["thinking"].(string); ok {
										contentStr += t
										isThought = true
									}
								} else if cType == "tool_use" {
									contentStr += renderToolUse(cMap)
								} else if cType == "tool_result" {
									contentStr += renderToolResult(cMap)
								} else {
									if t, ok := cMap["text"].(string); ok {
										contentStr += t
									}
								}
							}
						}
					}
					if contentStr != "" {
						conv.Messages = append(conv.Messages, models.Message{Role: role, Content: contentStr, IsThought: isThought})
					}
				} else if typ, ok := obj["type"].(string); ok && typ == "system" {
					if content, ok := obj["content"].(string); ok && content != "" {
						conv.Messages = append(conv.Messages, models.Message{Role: "system", Content: content})
					} else if contentMap, ok := obj["content"].(map[string]interface{}); ok {
						if text, ok := contentMap["text"].(string); ok && text != "" {
							conv.Messages = append(conv.Messages, models.Message{Role: "system", Content: text})
						}
					}
				} else if typ == "attachment" {
					if att, ok := obj["attachment"].(map[string]interface{}); ok {
						if stdout, ok := att["stdout"].(string); ok && stdout != "" {
							conv.Messages = append(conv.Messages, models.Message{Role: "tool", Content: stdout})
						}
					}
				}
			}
		}
	} else {
		// Legacy single JSON format
		var obj map[string]interface{}
		if err := json.Unmarshal(data, &obj); err == nil {
			if id, ok := obj["sessionId"].(string); ok {
				conv.ResumeID = id
			} else if id, ok := obj["session_id"].(string); ok {
				conv.ResumeID = id
			}
			if title, ok := obj["title"].(string); ok {
				conv.Title = title
			}
			if msgs, ok := obj["messages"].([]interface{}); ok {
				for _, m := range msgs {
					if mmap, ok := m.(map[string]interface{}); ok {
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
			}
		}
	}

	return conv, nil
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
