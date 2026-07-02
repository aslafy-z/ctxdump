package models

import (
	"testing"
	"unicode/utf8"
)

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"ascii within limit", "hello", 10, "hello"},
		{"ascii exact boundary", "hello", 5, "hello"},
		{"ascii truncated", "hello", 3, "hel"},
		{"cjk within limit", "你好世界", 10, "你好世界"},
		{"cjk exact boundary", "你好世界", 4, "你好世界"},
		{"cjk truncated", "你好世界", 2, "你好"},
		{"emoji within limit", "😀😀", 5, "😀😀"},
		{"emoji exact boundary", "😀😀", 2, "😀😀"},
		{"emoji truncated", "😀😀", 1, "😀"},
		{"max zero", "hello", 0, ""},
		{"max negative", "hello", -3, ""},
		{"empty string", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateRunes(tt.s, tt.max)
			if got != tt.want {
				t.Fatalf("TruncateRunes(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result %q is not valid UTF-8", got)
			}
			if tt.max > 0 && utf8.RuneCountInString(got) > tt.max {
				t.Errorf("result has %d runes, exceeds max %d", utf8.RuneCountInString(got), tt.max)
			}
		})
	}
}

func TestTruncateBytes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"ascii within limit", "hello", 10, "hello"},
		{"ascii exact boundary", "hello", 5, "hello"},
		{"ascii truncated", "hello", 3, "hel"},
		{"cjk within limit", "你好世界", 20, "你好世界"},
		{"cjk exact boundary", "你好世界", 12, "你好世界"},
		{"max zero", "hello", 0, ""},
		{"max negative", "hello", -3, ""},
		{"empty string", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateBytes(tt.s, tt.max)
			if got != tt.want {
				t.Fatalf("TruncateBytes(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result %q is not valid UTF-8", got)
			}
			if len(got) > tt.max && tt.max > 0 {
				t.Errorf("result is %d bytes, exceeds max %d", len(got), tt.max)
			}
		})
	}
}

// TestTruncateBytesMidRune verifies that when the byte budget lands inside a
// multibyte rune, the whole rune is dropped so the result stays valid UTF-8 and
// shorter than the requested budget.
func TestTruncateBytesMidRune(t *testing.T) {
	// "你好" is 6 bytes (3 bytes each). A budget of 4 falls inside the second
	// rune, so only the first rune (3 bytes) should survive.
	got := TruncateBytes("你好", 4)
	if got != "你" {
		t.Fatalf("TruncateBytes(%q, 4) = %q, want %q", "你好", got, "你")
	}
	if !utf8.ValidString(got) {
		t.Fatalf("result %q is not valid UTF-8", got)
	}
	if len(got) >= 4 {
		t.Fatalf("expected result shorter than budget 4, got %d bytes", len(got))
	}

	// Emoji "😀" is 4 bytes. A budget of 3 lands mid-rune and should drop it
	// entirely, yielding an empty string.
	got = TruncateBytes("😀", 3)
	if got != "" {
		t.Fatalf("TruncateBytes(%q, 3) = %q, want empty", "😀", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("result %q is not valid UTF-8", got)
	}
}
