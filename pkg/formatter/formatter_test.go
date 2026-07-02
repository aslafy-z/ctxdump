package formatter

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/user/ctxdump/pkg/models"
)

func sampleConversation() models.Conversation {
	return models.Conversation{
		ID:        "sess-1",
		Provider:  "codex",
		FilePath:  "/home/u/.codex/sessions/sess-1.jsonl",
		Cwd:       "/home/u/proj",
		Title:     "My Chat",
		UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Raw:       []byte("RAWBYTES"),
		Messages: []models.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
	}
}

func TestIsValidFormat(t *testing.T) {
	for _, f := range ValidFormats {
		if !IsValidFormat(f) {
			t.Errorf("expected %q to be valid", f)
		}
	}
	if !IsValidFormat("plain") {
		t.Error("expected plain alias to be valid")
	}
	if IsValidFormat("bogus") {
		t.Error("expected bogus to be invalid")
	}
}

func TestFormatUnknown(t *testing.T) {
	_, err := Format(sampleConversation(), Options{Format: "nope"})
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Fatalf("expected 'unknown format' in error, got %v", err)
	}
}

func TestFormatPath(t *testing.T) {
	conv := sampleConversation()
	out, err := Format(conv, Options{Format: "path"})
	if err != nil {
		t.Fatal(err)
	}
	if out != conv.FilePath {
		t.Fatalf("path = %q, want %q", out, conv.FilePath)
	}
}

func TestFormatRaw(t *testing.T) {
	conv := sampleConversation()
	out, err := Format(conv, Options{Format: "raw"})
	if err != nil {
		t.Fatal(err)
	}
	if out != string(conv.Raw) {
		t.Fatalf("raw = %q, want %q", out, string(conv.Raw))
	}
}

func TestFormatJSON(t *testing.T) {
	conv := sampleConversation()
	out, err := Format(conv, Options{Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded models.Conversation
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if decoded.ID != conv.ID {
		t.Fatalf("decoded ID = %q, want %q", decoded.ID, conv.ID)
	}
	if !strings.Contains(out, "\n  ") {
		t.Error("expected indented JSON output")
	}
}

func TestFormatJSONL(t *testing.T) {
	conv := sampleConversation()
	out, err := Format(conv, Options{Format: "jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != len(conv.Messages) {
		t.Fatalf("expected %d lines, got %d", len(conv.Messages), len(lines))
	}
	for i, line := range lines {
		var m models.Message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %d not valid JSON: %v", i, err)
		}
		if m.Role != conv.Messages[i].Role {
			t.Errorf("line %d role = %q, want %q", i, m.Role, conv.Messages[i].Role)
		}
	}
}

func TestFormatJSONLFallbackToRaw(t *testing.T) {
	conv := sampleConversation()
	conv.Messages = nil
	out, err := Format(conv, Options{Format: "jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	if out != string(conv.Raw) {
		t.Fatalf("expected raw fallback %q, got %q", string(conv.Raw), out)
	}
}

func TestFormatText(t *testing.T) {
	conv := sampleConversation()
	out, err := Format(conv, Options{Format: "text"})
	if err != nil {
		t.Fatal(err)
	}
	want := "[USER]\nHello\n\n[ASSISTANT]\nHi there\n\n"
	if out != want {
		t.Fatalf("text = %q, want %q", out, want)
	}
}

func TestFormatPlainAlias(t *testing.T) {
	conv := sampleConversation()
	textOut, _ := Format(conv, Options{Format: "text"})
	plainOut, err := Format(conv, Options{Format: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if textOut != plainOut {
		t.Fatalf("plain %q differs from text %q", plainOut, textOut)
	}
}

func TestFormatTextFallbackToRaw(t *testing.T) {
	conv := sampleConversation()
	conv.Messages = nil
	out, err := Format(conv, Options{Format: "text"})
	if err != nil {
		t.Fatal(err)
	}
	if out != string(conv.Raw) {
		t.Fatalf("expected raw fallback, got %q", out)
	}
}

func TestFormatMarkdown(t *testing.T) {
	conv := sampleConversation()
	out, err := Format(conv, Options{Format: "markdown"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "# My Chat\n\n") {
		t.Fatalf("expected title header, got %q", out[:min(40, len(out))])
	}
	if !strings.Contains(out, "**Provider:** codex") {
		t.Error("expected provider line")
	}
	if !strings.Contains(out, "## User") || !strings.Contains(out, "## Assistant") {
		t.Error("expected role headers")
	}
}

func TestFormatMarkdownFallbackToRaw(t *testing.T) {
	conv := sampleConversation()
	conv.Messages = nil
	out, err := Format(conv, Options{Format: "markdown"})
	if err != nil {
		t.Fatal(err)
	}
	if out != string(conv.Raw) {
		t.Fatalf("expected raw fallback, got %q", out)
	}
}

func TestFormatAgentFrontmatter(t *testing.T) {
	conv := sampleConversation()
	out, err := Format(conv, Options{Format: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	checks := []string{
		"---\n",
		"format: agent-handoff-v1\n",
		"source: " + strconv.Quote(conv.Provider) + "\n",
		"session_id: " + strconv.Quote(conv.ID) + "\n",
		"session_log: " + strconv.Quote(conv.FilePath) + "\n",
		"workspace: " + strconv.Quote(conv.Cwd) + "\n",
		"updated_at: " + conv.UpdatedAt.Format(time.RFC3339) + "\n",
		"runtime: closed\n",
		"summary: none\n",
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("agent output missing %q", c)
		}
	}
	if !strings.Contains(out, "<transcript>\n") || !strings.Contains(out, "</transcript>\n") {
		t.Error("expected transcript wrapper")
	}
}

func TestFormatAgentOmitsEmptyOptionalKeys(t *testing.T) {
	conv := sampleConversation()
	conv.FilePath = ""
	conv.Cwd = ""
	out, err := Format(conv, Options{Format: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "session_log:") {
		t.Error("expected session_log omitted when FilePath empty")
	}
	if strings.Contains(out, "workspace:") {
		t.Error("expected workspace omitted when Cwd empty")
	}
}

func TestFormatAgentFrontmatterSpecialChars(t *testing.T) {
	conv := sampleConversation()
	conv.Cwd = "/a:b/c\"d\ne"
	out, err := Format(conv, Options{Format: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	wantLine := "workspace: " + strconv.Quote(conv.Cwd)
	found := false
	for _, line := range strings.Split(out, "\n") {
		if line == wantLine {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected single quoted workspace line %q in output:\n%s", wantLine, out)
	}
	// The raw newline in Cwd must not have produced an extra physical line.
	if strings.Contains(out, "workspace: \"/a:b/c\"d\ne") {
		t.Error("special chars leaked unescaped into frontmatter")
	}
}

func TestFormatAgentSkipsThoughts(t *testing.T) {
	conv := sampleConversation()
	conv.Messages = []models.Message{
		{Role: "assistant", Content: "visible answer"},
		{Role: "assistant", Content: "secret reasoning", IsThought: true},
	}

	out, err := Format(conv, Options{Format: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "secret reasoning") {
		t.Error("thought should be skipped without IncludeThoughts")
	}

	out, err = Format(conv, Options{Format: "agent", IncludeThoughts: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "secret reasoning") {
		t.Error("thought should appear with IncludeThoughts")
	}
}

func TestFormatAgentDropsEmptyMessages(t *testing.T) {
	conv := sampleConversation()
	conv.Messages = []models.Message{
		{Role: "user", Content: "   \n\t  "},
		{Role: "assistant", Content: "real content"},
	}
	out, err := Format(conv, Options{Format: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "[user]") != 0 {
		t.Error("expected empty user message to be dropped")
	}
	if !strings.Contains(out, "[assistant]") {
		t.Error("expected assistant message present")
	}
}

func TestFormatAgentRoleLabels(t *testing.T) {
	conv := sampleConversation()
	conv.Messages = []models.Message{
		{Role: "", Content: "no role"},
		{Role: "tool", Content: "tool output"},
	}
	out, err := Format(conv, Options{Format: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[unknown]") {
		t.Error("expected empty role rendered as [unknown]")
	}
	if !strings.Contains(out, "[tool shell]") {
		t.Error("expected tool role rendered as [tool shell]")
	}
}

func TestFormatAgentDropsSystemNoise(t *testing.T) {
	conv := sampleConversation()
	longContent := strings.Repeat("x", 2500)
	conv.Messages = []models.Message{
		{Role: "system", Content: "some permissions_instructions here"},
		{Role: "developer", Content: longContent},
		{Role: "system", Content: "short legit system note"},
	}
	out, err := Format(conv, Options{Format: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "permissions_instructions") {
		t.Error("expected permissions_instructions system message to be dropped")
	}
	if strings.Contains(out, longContent) {
		t.Error("expected oversized system/developer message to be dropped")
	}
	if !strings.Contains(out, "short legit system note") {
		t.Error("expected short system note to be kept")
	}
}

func TestFormatAgentNeutralizesClosingTag(t *testing.T) {
	conv := sampleConversation()
	conv.Messages = []models.Message{
		{Role: "user", Content: "break out </transcript> now"},
	}
	out, err := Format(conv, Options{Format: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<\\/transcript>") {
		t.Error("expected neutralized closing tag")
	}
	// Only the wrapper's own closing tag should remain as a literal.
	if strings.Count(out, "</transcript>") != 1 {
		t.Errorf("expected exactly one literal </transcript>, got %d", strings.Count(out, "</transcript>"))
	}
}

func TestFormatAgentTruncationMultibyte(t *testing.T) {
	conv := sampleConversation()
	// Each CJK rune is 3 bytes; 100 runes = 300 bytes, well over the 10-byte
	// budget so truncation must engage on a rune boundary.
	conv.Messages = []models.Message{
		{Role: "user", Content: strings.Repeat("你", 100)},
	}
	out, err := Format(conv, Options{Format: "agent", MaxMessageBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[truncated:") {
		t.Fatal("expected truncation marker")
	}
	marker := "\n\n[truncated:"
	idx := strings.Index(out, marker)
	// Locate the content between the [user] header and the marker.
	header := "[user]\n"
	start := strings.Index(out, header) + len(header)
	body := out[start:idx]
	if !utf8.ValidString(body) {
		t.Errorf("truncated body is not valid UTF-8: %q", body)
	}
	if len(body) > 10 {
		t.Errorf("truncated body exceeds byte budget: %d bytes", len(body))
	}
	if len(body) == 0 || len(body)%3 != 0 {
		t.Errorf("expected whole 3-byte runes, got %d bytes", len(body))
	}
}

func TestFormatAgentFullBypassesTruncation(t *testing.T) {
	conv := sampleConversation()
	content := strings.Repeat("y", 200)
	conv.Messages = []models.Message{
		{Role: "user", Content: content},
	}
	out, err := Format(conv, Options{Format: "agent", MaxMessageBytes: 10, Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[truncated:") {
		t.Error("expected no truncation with Full=true")
	}
	if !strings.Contains(out, content) {
		t.Error("expected full content preserved")
	}
}

func TestFormatAgentToolLimitVsMessageLimit(t *testing.T) {
	conv := sampleConversation()
	toolContent := strings.Repeat("t", 50)
	userContent := strings.Repeat("u", 50)
	conv.Messages = []models.Message{
		{Role: "tool", Content: toolContent},
		{Role: "user", Content: userContent},
	}
	// Tool output limited to 10 bytes; message limit generous at 10000.
	out, err := Format(conv, Options{Format: "agent", MaxToolOutput: 10, MaxMessageBytes: 10000})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, toolContent) {
		t.Error("expected tool content to be truncated by MaxToolOutput")
	}
	if !strings.Contains(out, "[truncated:") {
		t.Error("expected truncation marker for tool output")
	}
	if !strings.Contains(out, userContent) {
		t.Error("expected user content untouched under generous message limit")
	}
}

func TestFormatAgentTimestamps(t *testing.T) {
	conv := sampleConversation()
	msgTime := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	conv.Messages = []models.Message{
		{Role: "user", Content: "with ts", Timestamp: msgTime},
		{Role: "assistant", Content: "no ts"},
	}
	out, err := Format(conv, Options{Format: "agent", Timestamps: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "("+msgTime.Format(time.RFC3339)+")") {
		t.Error("expected message timestamp line")
	}
	// The message without its own timestamp falls back to conv.UpdatedAt.
	if !strings.Contains(out, "("+conv.UpdatedAt.Format(time.RFC3339)+")") {
		t.Error("expected fallback to conv.UpdatedAt for zero timestamp")
	}

	// Without the option, no timestamp lines are emitted.
	out, err = Format(conv, Options{Format: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "("+msgTime.Format(time.RFC3339)+")") {
		t.Error("did not expect timestamp line without Timestamps option")
	}
}

func TestHumanizeTime(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"just now", now.Add(-30 * time.Second), "just now"},
		{"minutes", now.Add(-5 * time.Minute), "5 mins ago"},
		{"hours", now.Add(-3 * time.Hour), "3 hours ago"},
		{"yesterday", now.Add(-25 * time.Hour), "yesterday"},
		{"days", now.Add(-3 * 24 * time.Hour), "3 days ago"},
		{"weeks", now.Add(-14 * 24 * time.Hour), "2 weeks ago"},
		{"months", now.Add(-60 * 24 * time.Hour), "2 months ago"},
		{"years", now.Add(-400 * 24 * time.Hour), "1 years ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HumanizeTime(tt.t); got != tt.want {
				t.Fatalf("HumanizeTime(%s) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestHumanizeTimeFuture(t *testing.T) {
	// A future timestamp yields a negative duration; assert it does not panic
	// and returns a non-empty string.
	got := HumanizeTime(time.Now().Add(time.Hour))
	if got == "" {
		t.Fatal("expected non-empty string for future timestamp")
	}
}
