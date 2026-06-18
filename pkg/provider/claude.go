package provider

import (
	"bufio"
	"encoding/json"
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
			title, snippet := extractClaudeMeta(path)
			cwd := ""
			if strings.Contains(path, ".claude/projects/") {
				dir := filepath.Dir(path)
				base := filepath.Base(dir)
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

func extractClaudeMeta(path string) (string, string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var title string
	var firstMessage string
	for i := 0; i < 200 && scanner.Scan(); i++ {
		line := scanner.Text()
		if title == "" && strings.Contains(line, `"aiTitle":`) {
			var obj map[string]interface{}
			if json.Unmarshal([]byte(line), &obj) == nil {
				if t, ok := obj["aiTitle"].(string); ok {
					title = t
				}
			}
		}
		if firstMessage == "" && strings.Contains(line, `"role":"user"`) {
			var obj map[string]interface{}
			if json.Unmarshal([]byte(line), &obj) == nil {
				if msgObj, ok := obj["message"].(map[string]interface{}); ok {
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
	if firstMessage != "" {
		if strings.HasPrefix(firstMessage, "<local-command-caveat>") {
			endIdx := strings.Index(firstMessage, "</local-command-caveat>")
			if endIdx != -1 {
				firstMessage = strings.TrimSpace(firstMessage[endIdx+len("</local-command-caveat>"):])
			} else {
				// If no closing tag, try to skip a reasonable chunk
				firstMessage = ""
			}
		}
		
		firstMessage = strings.ReplaceAll(firstMessage, "\n", " ")
		if len(firstMessage) > 100 {
			firstMessage = firstMessage[:97] + "..."
		}
	}
	return title, firstMessage
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

	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err == nil {
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

	return conv, nil
}
