package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/ctxdump/pkg/models"
)

type geminiProvider struct{}

func NewGeminiProvider() Provider {
	return &geminiProvider{}
}

func (p *geminiProvider) Name() string {
	return "gemini"
}

func (p *geminiProvider) List(opts Options) ([]models.Conversation, error) {
	paths := p.getPaths(opts)
	var conversations []models.Conversation

	for _, dir := range paths {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				// Skip the Gemini CLI's bundled-binary cache; it holds no conversations.
				if d.Name() == "bin" {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".json") && !strings.HasSuffix(d.Name(), ".jsonl") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			title := ""
			snippet := ""
			resumeID := ""
			cwd := ""
			if c, err := p.parseFile(path); err == nil {
				title = c.Title
				resumeID = c.ResumeID
				cwd = c.Cwd
				for _, m := range c.Messages {
					if m.Role == "user" && m.Content != "" {
						snippet = m.Content
						break
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
				ID:        geminiID(dir, path, d.Name()),
				Provider:  p.Name(),
				FilePath:  path,
				Title:     title,
				Snippet:   snippet,
				Cwd:       cwd,
				ResumeID:  resumeID,
				UpdatedAt: info.ModTime(),
			})
			return nil
		})
		if err != nil {
			continue
		}
	}
	return conversations, nil
}

// geminiID derives a stable, collision-free listing id: the per-session hash
// directory for a `logs.json`, otherwise the (already unique) chat filename.
func geminiID(dir, path, name string) string {
	if name == "logs.json" {
		if rel, err := filepath.Rel(dir, path); err == nil {
			parts := strings.Split(rel, string(filepath.Separator))
			if len(parts) > 0 && parts[0] != "" {
				return parts[0]
			}
		}
	}
	return name
}

func (p *geminiProvider) Dump(idOrFile string, opts Options) (models.Conversation, error) {
	if _, err := os.Stat(idOrFile); err == nil {
		return p.parseFile(idOrFile)
	}

	convs, err := p.List(opts)
	if err == nil {
		for _, c := range convs {
			if c.ID == idOrFile {
				return p.parseFile(c.FilePath)
			}
		}
	}

	return models.Conversation{}, os.ErrNotExist
}

func (p *geminiProvider) getPaths(opts Options) []string {
	if opts.CustomPath != "" {
		return []string{opts.CustomPath}
	}
	var dirs []string
	if home, _ := os.UserHomeDir(); home != "" {
		dirs = append(dirs, filepath.Join(home, ".gemini", "tmp"))
	}
	return dirs
}

func (p *geminiProvider) parseFile(path string) (models.Conversation, error) {
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

	trimmed := bytes.TrimSpace(data)
	switch {
	case len(trimmed) > 0 && trimmed[0] == '[':
		parseGeminiLogs(&conv, data)
	default:
		var obj map[string]interface{}
		if json.Unmarshal(data, &obj) == nil {
			if _, ok := obj["messages"].([]interface{}); ok {
				parseGeminiObject(&conv, obj)
				break
			}
		}
		parseGeminiChats(&conv, data)
	}

	return conv, nil
}

// parseGeminiLogs handles the top-level `logs.json` array of
// `{sessionId, type, message, timestamp}` entries.
func parseGeminiLogs(conv *models.Conversation, data []byte) {
	var entries []map[string]interface{}
	if json.Unmarshal(data, &entries) != nil {
		return
	}
	var newest time.Time
	for _, e := range entries {
		if conv.ResumeID == "" {
			if id, ok := e["sessionId"].(string); ok {
				conv.ResumeID = id
			}
		}
		msg, _ := e["message"].(string)
		if msg == "" {
			continue
		}
		ts := parseRFC3339(e["timestamp"])
		if ts.After(newest) {
			newest = ts
		}
		conv.Messages = append(conv.Messages, models.Message{
			Role: geminiRole(e["type"]), Content: msg, Timestamp: ts,
		})
	}
	if !newest.IsZero() {
		conv.UpdatedAt = newest
	}
}

// parseGeminiChats handles the `chats/session-*.jsonl` shape: a header line
// followed by `{"$set":{"messages":[...]}}` snapshots. The last snapshot wins.
func parseGeminiChats(conv *models.Conversation, data []byte) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)

	first := true
	var lastMessages []interface{}
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec map[string]interface{}
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		if first {
			first = false
			if id, ok := rec["sessionId"].(string); ok {
				conv.ResumeID = id
			}
			if lu := parseRFC3339(rec["lastUpdated"]); !lu.IsZero() {
				conv.UpdatedAt = lu
			}
		}
		if set, ok := rec["$set"].(map[string]interface{}); ok {
			if msgs, ok := set["messages"].([]interface{}); ok {
				lastMessages = msgs
			}
		}
	}

	for _, m := range lastMessages {
		mm, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		content := joinTextBlocks(mm["content"])
		if content == "" {
			continue
		}
		if conv.Cwd == "" {
			if c := extractGeminiWorkspace(content); c != "" {
				conv.Cwd = c
			}
		}
		conv.Messages = append(conv.Messages, models.Message{
			Role:      geminiRole(mm["type"]),
			Content:   content,
			Timestamp: parseRFC3339(mm["timestamp"]),
		})
	}
}

// parseGeminiObject handles the legacy whole-file object with a `messages` array.
func parseGeminiObject(conv *models.Conversation, obj map[string]interface{}) {
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
		mm, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := mm["role"].(string)
		content := joinTextBlocks(mm["content"])
		if content != "" {
			conv.Messages = append(conv.Messages, models.Message{Role: role, Content: content})
		}
	}
}

// geminiRole maps a gemini message type to a normalized role.
func geminiRole(v interface{}) string {
	if t, ok := v.(string); ok && t == "user" {
		return "user"
	}
	return "assistant"
}

// extractGeminiWorkspace pulls the first workspace directory out of the
// `session_context` block's "Workspace Directories" listing.
func extractGeminiWorkspace(content string) string {
	idx := strings.Index(content, "Workspace Directories:")
	if idx == -1 {
		return ""
	}
	lines := strings.Split(content[idx:], "\n")
	for _, l := range lines[1:] {
		l = strings.TrimSpace(l)
		l = strings.TrimPrefix(l, "-")
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "/") {
			return l
		}
	}
	return ""
}

func (p *geminiProvider) ResumeSpec(
	conv models.Conversation,
	opts Options,
	prompt []string,
) (ResumeSpec, error) {
	resumeID := conv.ResumeID
	if resumeID == "" {
		resumeID = strings.TrimSuffix(conv.ID, ".jsonl")
		resumeID = strings.TrimSuffix(resumeID, ".json")
	}

	args := []string{"resume"}
	if resumeID != "" {
		args = append(args, resumeID)
	}
	args = append(args, prompt...)

	return ResumeSpec{
		Command: "gemini",
		Args:    args,
		Dir:     conv.Cwd,
	}, nil
}
