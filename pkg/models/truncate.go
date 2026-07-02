package models

import "unicode/utf8"

// TruncateRunes returns s limited to at most max runes, cutting on a rune
// boundary. It never emits an ellipsis; callers append their own marker. A
// non-positive max yields an empty string.
func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	count := 0
	for i := range s {
		if count == max {
			return s[:i]
		}
		count++
	}
	return s
}

// TruncateBytes returns s limited to at most max bytes without splitting a
// multibyte rune. When the byte budget falls inside a rune, the whole rune is
// dropped so the result stays valid UTF-8. A non-positive max yields an empty
// string.
func TruncateBytes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}
