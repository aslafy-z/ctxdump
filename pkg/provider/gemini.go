package provider

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
				if c, err := p.parseFile(path); err == nil {
					title = c.Title
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
						snippet = snippet[:97] + "..."
					}
					if title == "" {
						title = snippet
						if len(title) > 50 {
							title = title[:47] + "..."
						}
					}
				}
				id := d.Name()
				rel, err := filepath.Rel(dir, path)
				if err == nil {
					parts := strings.Split(rel, string(filepath.Separator))
					if len(parts) > 1 {
						id = parts[0]
					}
				}
				conversations = append(conversations, models.Conversation{
					ID:        id,
					Provider:  p.Name(),
					FilePath:  path,
					Title:     title,
					Snippet:   snippet,
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
					content, _ := mmap["content"].(string)
					if content != "" {
						conv.Messages = append(conv.Messages, models.Message{Role: role, Content: content})
					}
				}
			}
		}
	}

	return conv, nil
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

	dir := conv.Cwd
	if dir == "" {
		dir = "" // Let execution use the current working directory
	}

	return ResumeSpec{
		Command: "gemini",
		Args:    args,
		Dir:     dir,
	}, nil
}
