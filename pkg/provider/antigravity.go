package provider

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/ctxdump/pkg/models"
)

type antigravityProvider struct{}

func NewAntigravityProvider() Provider {
	return &antigravityProvider{}
}

func (p *antigravityProvider) Name() string {
	return "antigravity"
}

func (p *antigravityProvider) List(opts Options) ([]models.Conversation, error) {
	paths := p.getPaths(opts)
	var conversations []models.Conversation

	for _, brainDir := range paths {
		entries, err := os.ReadDir(brainDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			convID := entry.Name()
			transcriptPath := filepath.Join(brainDir, convID, ".system_generated", "logs", "transcript.jsonl")
			if _, err := os.Stat(transcriptPath); err != nil {
				continue
			}

			title, snippet, cwd, updatedAt := p.extractMeta(transcriptPath)

			if title == "" {
				title = snippet
				if len(title) > 50 {
					title = title[:47] + "..."
				}
			}
			if title == "" {
				title = convID
			}

			conversations = append(conversations, models.Conversation{
				ID:        convID,
				Provider:  p.Name(),
				FilePath:  transcriptPath,
				Title:     title,
				Snippet:   snippet,
				Cwd:       cwd,
				UpdatedAt: updatedAt,
			})
		}
	}
	return conversations, nil
}

func (p *antigravityProvider) Dump(idOrFile string, opts Options) (models.Conversation, error) {
	// Direct path to transcript file
	if _, err := os.Stat(idOrFile); err == nil {
		return p.parseTranscript(idOrFile)
	}

	// Search by conversation UUID
	paths := p.getPaths(opts)
	for _, brainDir := range paths {
		transcriptPath := filepath.Join(brainDir, idOrFile, ".system_generated", "logs", "transcript.jsonl")
		if _, err := os.Stat(transcriptPath); err == nil {
			return p.parseTranscript(transcriptPath)
		}
	}

	// Fallback: check if idOrFile matches any discovered conversation
	convs, err := p.List(opts)
	if err == nil {
		for _, c := range convs {
			if c.ID == idOrFile {
				return p.parseTranscript(c.FilePath)
			}
		}
	}

	return models.Conversation{}, os.ErrNotExist
}

func (p *antigravityProvider) getPaths(opts Options) []string {
	if opts.CustomPath != "" {
		return []string{opts.CustomPath}
	}
	var dirs []string
	if home, _ := os.UserHomeDir(); home != "" {
		dirs = append(dirs, filepath.Join(home, ".gemini", "antigravity-ide", "brain"))
		dirs = append(dirs, filepath.Join(home, ".gemini", "antigravity", "brain"))
	}
	return dirs
}

// extractMeta does a lightweight scan of the first ~50 lines of a transcript
// to extract title, snippet, cwd, and timestamp metadata without reading the entire file.
func (p *antigravityProvider) extractMeta(transcriptPath string) (title, snippet, cwd string, updatedAt time.Time) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		info, _ := os.Stat(transcriptPath)
		if info != nil {
			updatedAt = info.ModTime()
		}
		return
	}
	defer f.Close()

	// Fallback to file mtime
	info, _ := f.Stat()
	if info != nil {
		updatedAt = info.ModTime()
	}

	scanner := bufio.NewScanner(f)
	// Increase scanner buffer for potentially large lines
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	lineCount := 0
	for scanner.Scan() && lineCount < 80 {
		lineCount++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var step transcriptStep
		if err := json.Unmarshal(line, &step); err != nil {
			continue
		}

		// Track the latest timestamp
		if !step.CreatedAt.IsZero() {
			updatedAt = step.CreatedAt
		}

		// Extract user message for title/snippet
		if step.Source == "USER_EXPLICIT" && step.Type == "USER_INPUT" && step.Content != "" {
			userMsg := extractUserRequest(step.Content)
			if userMsg != "" && snippet == "" {
				snippet = strings.ReplaceAll(userMsg, "\n", " ")
				snippet = strings.TrimSpace(snippet)
				if len(snippet) > 100 {
					snippet = snippet[:97] + "..."
				}
			}
		}

		// Extract CWD from metadata in user input as a fallback
		if step.Source == "USER_EXPLICIT" && step.Type == "USER_INPUT" && cwd == "" {
			cwd = extractCwdFromMetadata(step.Content)
		}

		// Prefer CWD from early tool calls (e.g. run_command or list_dir)
		for _, call := range step.ToolCalls {
			if cwdRaw, ok := call.Args["Cwd"]; ok && cwdRaw != "" {
				cwd = strings.Trim(cwdRaw, `"`)
				break
			}
			if dirRaw, ok := call.Args["DirectoryPath"]; ok && dirRaw != "" {
				cwd = strings.Trim(dirRaw, `"`)
				break
			}
		}
	}

	// Scan backward for the last timestamp by reading the rest of the file
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var step transcriptStep
		if err := json.Unmarshal(line, &step); err != nil {
			continue
		}
		if !step.CreatedAt.IsZero() {
			updatedAt = step.CreatedAt
		}
	}

	return
}

// parseTranscript reads and parses an entire transcript.jsonl file into a Conversation.
func (p *antigravityProvider) parseTranscript(transcriptPath string) (models.Conversation, error) {
	data, err := os.ReadFile(transcriptPath)
	if err != nil {
		return models.Conversation{}, err
	}

	info, err := os.Stat(transcriptPath)
	if err != nil {
		return models.Conversation{}, err
	}

	// Derive conversation ID from the directory structure:
	// .../brain/<uuid>/.system_generated/logs/transcript.jsonl
	convID := filepath.Base(transcriptPath) // "transcript.jsonl"
	dir := filepath.Dir(transcriptPath)     // .../logs
	dir = filepath.Dir(dir)                 // .../system_generated (actually ".system_generated")
	dir = filepath.Dir(dir)                 // .../<uuid>
	convID = filepath.Base(dir)

	conv := models.Conversation{
		ID:        convID,
		Provider:  p.Name(),
		FilePath:  transcriptPath,
		UpdatedAt: info.ModTime(),
		Raw:       data,
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var step transcriptStep
		if err := json.Unmarshal(line, &step); err != nil {
			continue
		}

		// Track latest timestamp
		if !step.CreatedAt.IsZero() {
			conv.UpdatedAt = step.CreatedAt
		}

		switch {
		case step.Source == "USER_EXPLICIT" && step.Type == "USER_INPUT":
			userMsg := extractUserRequest(step.Content)
			if userMsg != "" {
				conv.Messages = append(conv.Messages, models.Message{
					Role:    "user",
					Content: strings.TrimSpace(userMsg),
				})
			}
			// Extract CWD from first user input
			if conv.Cwd == "" {
				conv.Cwd = extractCwdFromMetadata(step.Content)
			}
			// Extract title from first user message
			if conv.Title == "" && userMsg != "" {
				t := strings.ReplaceAll(strings.TrimSpace(userMsg), "\n", " ")
				if len(t) > 50 {
					t = t[:47] + "..."
				}
				conv.Title = t
			}

		case step.Source == "MODEL" && step.Type == "PLANNER_RESPONSE":
			// Prefer CWD from early tool calls
			if conv.Cwd == "" || strings.Contains(conv.Cwd, ".gemini/antigravity") {
				for _, call := range step.ToolCalls {
					if cwdRaw, ok := call.Args["Cwd"]; ok && cwdRaw != "" {
						conv.Cwd = strings.Trim(cwdRaw, `"`)
						break
					}
					if dirRaw, ok := call.Args["DirectoryPath"]; ok && dirRaw != "" {
						conv.Cwd = strings.Trim(dirRaw, `"`)
						break
					}
				}
			}
			// Add thinking as a thought message
			if step.Thinking != "" {
				conv.Messages = append(conv.Messages, models.Message{
					Role:      "assistant",
					Content:   strings.TrimSpace(step.Thinking),
					IsThought: true,
				})
			}
			// Add content as the main assistant response
			if step.Content != "" {
				conv.Messages = append(conv.Messages, models.Message{
					Role:    "assistant",
					Content: strings.TrimSpace(step.Content),
				})
			}

		case step.Type == "VIEW_FILE" || step.Type == "RUN_COMMAND" ||
			step.Type == "LIST_DIRECTORY" || step.Type == "CODE_ACTION" ||
			step.Type == "MCP_TOOL" || step.Type == "GENERIC":
			if step.Content != "" {
				conv.Messages = append(conv.Messages, models.Message{
					Role:    "tool",
					Content: strings.TrimSpace(step.Content),
				})
			}

		case step.Type == "ERROR_MESSAGE":
			errContent := step.Content
			if errContent == "" {
				errContent = step.Error
			}
			if errContent != "" {
				conv.Messages = append(conv.Messages, models.Message{
					Role:    "system",
					Content: strings.TrimSpace(errContent),
				})
			}

		// Skip system noise: CONVERSATION_HISTORY, KNOWLEDGE_ARTIFACTS, EPHEMERAL_MESSAGE, SYSTEM_MESSAGE
		}
	}

	return conv, nil
}

// transcriptStep represents a single line from transcript.jsonl.
type transcriptStep struct {
	StepIndex int       `json:"step_index"`
	Source    string    `json:"source"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	Content   string    `json:"content"`
	Thinking  string    `json:"thinking"`
	Error     string    `json:"error"`
	ToolCalls []toolCall `json:"tool_calls"`
}

type toolCall struct {
	Name string            `json:"name"`
	Args map[string]string `json:"args"`
}

// extractUserRequest pulls the content between <USER_REQUEST> tags,
// stripping <ADDITIONAL_METADATA> and <USER_SETTINGS_CHANGE> blocks.
func extractUserRequest(content string) string {
	start := strings.Index(content, "<USER_REQUEST>")
	end := strings.Index(content, "</USER_REQUEST>")
	
	if start != -1 {
		if end != -1 && end > start {
			content = content[start+len("<USER_REQUEST>") : end]
		} else {
			content = content[start+len("<USER_REQUEST>"):]
		}
	}
	
	// Strip metadata tags even if they are truncated
	for _, tag := range []string{"ADDITIONAL_METADATA", "USER_SETTINGS_CHANGE", "EPHEMERAL_MESSAGE"} {
		startTag := "<" + tag + ">"
		endTag := "</" + tag + ">"
		
		for {
			ts := strings.Index(content, startTag)
			if ts == -1 {
				break
			}
			te := strings.Index(content, endTag)
			if te == -1 || te < ts {
				// Truncated, remove everything from the tag onwards
				content = content[:ts]
				break
			}
			content = content[:ts] + content[te+len(endTag):]
		}
	}

	return strings.TrimSpace(content)
}

// extractCwdFromMetadata tries to find workspace/project path from ADDITIONAL_METADATA.
func extractCwdFromMetadata(content string) string {
	metaStart := strings.Index(content, "<ADDITIONAL_METADATA>")
	metaEnd := strings.Index(content, "</ADDITIONAL_METADATA>")
	if metaStart == -1 || metaEnd == -1 || metaEnd <= metaStart {
		return ""
	}
	meta := content[metaStart+len("<ADDITIONAL_METADATA>") : metaEnd]

	// Look for "Active Document:" line and extract the project directory
	for _, line := range strings.Split(meta, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Active Document:") {
			path := strings.TrimPrefix(line, "Active Document:")
			path = strings.TrimSpace(path)
			// Remove language suffix like " (LANGUAGE_GO)"
			if idx := strings.Index(path, " ("); idx != -1 {
				path = path[:idx]
			}
			// Skip untitled/virtual files
			if path == "" || strings.HasPrefix(path, "/Untitled") || !strings.HasPrefix(path, "/") {
				continue
			}
			// Return the parent directory as CWD (project root approximation)
			// Go up to a reasonable project root (look for common depth)
			dir := filepath.Dir(path)
			return dir
		}
	}
	return ""
}

// stripXMLBlock removes a <tag>...</tag> block from content.
func stripXMLBlock(content, tag string) string {
	startTag := "<" + tag + ">"
	endTag := "</" + tag + ">"
	for {
		start := strings.Index(content, startTag)
		if start == -1 {
			break
		}
		end := strings.Index(content, endTag)
		if end == -1 || end < start {
			break
		}
		content = content[:start] + content[end+len(endTag):]
	}
	return content
}

// findTranscripts walks a directory looking for transcript.jsonl files
// following the pattern: <dir>/<uuid>/.system_generated/logs/transcript.jsonl
func findTranscripts(dir string) []string {
	var results []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		transcriptPath := filepath.Join(dir, entry.Name(), ".system_generated", "logs", "transcript.jsonl")
		if _, err := os.Stat(transcriptPath); err == nil {
			results = append(results, transcriptPath)
		}
	}
	return results
}

// walkBrainDir is a specialized walker for brain directories that avoids
// recursing into deep directory structures. It only looks one level deep.
func (p *antigravityProvider) walkBrainDir(brainDir string, fn func(convID string, transcriptPath string)) {
	entries, err := os.ReadDir(brainDir)
	if err != nil {
		return
	}
	// Skip known non-conversation directories
	skipDirs := map[string]bool{
		"scratch": true, "plugins": true, "mcp": true,
		"conversations": true, "knowledge": true, "context_state": true,
		"code_tracker": true, "html_artifacts": true, "implicit": true,
		"playground": true, "annotations": true, "bin": true,
	}
	for _, entry := range entries {
		if !entry.IsDir() || skipDirs[entry.Name()] {
			continue
		}
		transcriptPath := filepath.Join(brainDir, entry.Name(), ".system_generated", "logs", "transcript.jsonl")
		if info, err := os.Stat(transcriptPath); err == nil && !info.IsDir() {
			fn(entry.Name(), transcriptPath)
		}
	}
}

// Ensure antigravityProvider does not accidentally walk into non-brain directories
// when using WalkDir by restricting to known brain directory structures.
var _ Provider = (*antigravityProvider)(nil)

// Helper to check if a path looks like a brain directory (contains UUID dirs with transcripts).
func isBrainDir(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t := filepath.Join(path, e.Name(), ".system_generated", "logs", "transcript.jsonl")
		if _, err := os.Stat(t); err == nil {
			return true
		}
	}
	return false
}

// CustomPath support: if --path points to a single conversation dir or a brain dir,
// handle both cases gracefully.
func (p *antigravityProvider) resolveCustomPath(customPath string) []string {
	// Check if this is a brain directory (contains UUID subdirs)
	if isBrainDir(customPath) {
		return []string{customPath}
	}

	// Check if it's a single conversation directory
	transcript := filepath.Join(customPath, ".system_generated", "logs", "transcript.jsonl")
	if _, err := os.Stat(transcript); err == nil {
		// It's a single conversation dir — wrap it so List/Dump can find it
		return []string{filepath.Dir(customPath)}
	}

	// Maybe it's a parent dir that contains brain/
	brainPath := filepath.Join(customPath, "brain")
	if isBrainDir(brainPath) {
		return []string{brainPath}
	}

	// Walk to discover any transcript.jsonl files
	var results []string
	filepath.WalkDir(customPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Name() == "transcript.jsonl" && !d.IsDir() {
			// Go up to the brain dir level
			// path = .../brain/<uuid>/.system_generated/logs/transcript.jsonl
			logsDir := filepath.Dir(path)       // .../logs
			sysGenDir := filepath.Dir(logsDir)  // .../.system_generated
			convDir := filepath.Dir(sysGenDir)  // .../<uuid>
			brainDir := filepath.Dir(convDir)   // .../brain
			if !contains(results, brainDir) {
				results = append(results, brainDir)
			}
		}
		return nil
	})
	if len(results) > 0 {
		return results
	}

	return []string{customPath}
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
