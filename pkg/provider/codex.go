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
						snippet = snippet[:97] + "..."
					}

					if title == "" {
						title = snippet
						if len(title) > 50 {
							title = title[:47] + "..."
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
	info, err := os.Stat(path)
	modTime := info.ModTime()

	conv := models.Conversation{
		ID:        filepath.Base(path),
		Provider:  p.Name(),
		FilePath:  path,
		UpdatedAt: modTime,
		Raw:       data,
	}

	// Try best-effort parsing (dumb mode)
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err == nil {
		if id, ok := obj["sessionId"].(string); ok {
			conv.ResumeID = id
		} else if id, ok := obj["session_id"].(string); ok {
			conv.ResumeID = id
		} else if id, ok := obj["id"].(string); ok {
			conv.ResumeID = id
		}
		if title, ok := obj["title"].(string); ok {
			conv.Title = title
		}
		if msgs, ok := obj["messages"].([]interface{}); ok {
			for _, m := range msgs {
				if mmap, ok := m.(map[string]interface{}); ok {
					role, _ := mmap["role"].(string)
					content, _ := mmap["content"].(string)
					if content != "" {
						start := strings.Index(content, "<cwd>")
						end := strings.Index(content, "</cwd>")
						if start != -1 && end != -1 && start+5 < end {
							conv.Cwd = content[start+5 : end]
						}

						content = StripSystemTags(content)
						if content != "" {
							conv.Messages = append(conv.Messages, models.Message{Role: role, Content: content})
						}
					}
				}
			}
		}
	} else {
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var mobj map[string]interface{}
			if err := json.Unmarshal(line, &mobj); err == nil {
				if conv.ResumeID == "" {
					if id, ok := mobj["sessionId"].(string); ok {
						conv.ResumeID = id
					} else if id, ok := mobj["session_id"].(string); ok {
						conv.ResumeID = id
					} else if id, ok := mobj["id"].(string); ok {
						conv.ResumeID = id
					}
				}
				if title, ok := mobj["title"].(string); ok && conv.Title == "" {
					conv.Title = title
				}
				role, _ := mobj["role"].(string)
				content, _ := mobj["content"].(string)

				isThought := false
				if payload, ok := mobj["payload"].(map[string]interface{}); ok {
					if r, ok := payload["role"].(string); ok {
						role = r
					}

					// Tool activity (commands, patches, MCP calls) lives in dedicated
					// payload types that carry no "content"/"message" field. Render them
					// explicitly so the work performed is visible in the transcript.
					if tool := renderCodexTool(payload); tool != "" {
						conv.Messages = append(conv.Messages, models.Message{Role: "tool", Content: tool})
						continue
					}

					if t, ok := payload["type"].(string); ok {
						if t == "user_message" {
							role = "user"
						} else if t == "thought" || t == "commentary" || t == "thinking" {
							isThought = true
							if role == "" {
								role = "assistant"
							}
						}
					}

					if c, ok := payload["content"].([]interface{}); ok && len(c) > 0 {
						if cObj, ok := c[0].(map[string]interface{}); ok {
							if text, ok := cObj["text"].(string); ok {
								content = text
							}
						}
					} else if msg, ok := payload["message"].(string); ok {
						content = msg
					}
				}

				// Sometimes thoughts don't have a payload type but are identified by empty role or "thought" role
				if role == "thought" || role == "thinking" {
					isThought = true
					role = "assistant"
				} else if role == "" && content != "" {
					// In codex, empty role with content is almost always commentary/thought
					isThought = true
				}

				if content != "" {
					start := strings.Index(content, "<cwd>")
					end := strings.Index(content, "</cwd>")
					if start != -1 && end != -1 && start+5 < end {
						conv.Cwd = content[start+5 : end]
					}

					content = StripSystemTags(content)
					if content != "" {
						conv.Messages = append(conv.Messages, models.Message{Role: role, Content: content, IsThought: isThought})
					}
				}
			}
		}
	}

	return conv, nil
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
