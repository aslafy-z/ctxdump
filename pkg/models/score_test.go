package models

import (
	"os"
	"testing"
	"time"
)

func TestCwdScore(t *testing.T) {
	tests := []struct {
		name    string
		current string
		target  string
		want    int
	}{
		{"empty target", "/home/user/proj", "", -1},
		{"exact match", "/home/user/proj", "/home/user/proj", 10000},
		{"exact match after clean", "/home/user/proj", "/home/user/proj/", 10000},
		// Common components: "", "home", "user" => (0+1)+(4+1)+(4+1) = 11.
		// score = 11*100 - len("/home/user/other") = 1100 - 16 = 1084.
		{"sibling directory", "/home/user/proj", "/home/user/other", 1084},
		// "/foo" vs "/foobar": only the root component matches (component-wise),
		// "foo" != "foobar". commonLen = 1 => 100 - len("/foobar") = 100 - 7 = 93.
		{"prefix but different component", "/foo", "/foobar", 93},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cwdScore(tt.current, tt.target)
			if got != tt.want {
				t.Fatalf("cwdScore(%q, %q) = %d, want %d", tt.current, tt.target, got, tt.want)
			}
		})
	}
}

func TestComputeSortScoreProximity(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	now := time.Now()

	exact := Conversation{Cwd: cwd, UpdatedAt: now}
	far := Conversation{Cwd: "/totally/unrelated/place", UpdatedAt: now}

	exactScore := ComputeSortScore(exact, cwd)
	farScore := ComputeSortScore(far, cwd)

	if exactScore <= farScore {
		t.Fatalf("expected exact cwd match to score higher: exact=%f far=%f", exactScore, farScore)
	}
}

func TestComputeSortScoreEmptyCwd(t *testing.T) {
	// An empty Cwd yields cwdScore -1, clamped to a proximity bonus of 0, so the
	// score is purely the negative age in days.
	past := time.Now().Add(-48 * time.Hour)
	c := Conversation{Cwd: "", UpdatedAt: past}
	got := ComputeSortScore(c, "/home/user/proj")

	if got > 0 {
		t.Fatalf("expected non-positive score for empty cwd in the past, got %f", got)
	}
	// Should be close to -2 days (allow slack for elapsed test time).
	if got < -3 || got > -1 {
		t.Fatalf("expected score near -2 days, got %f", got)
	}
}

func TestSortConversationsByScore(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	now := time.Now()

	convs := []Conversation{
		{ID: "far", Cwd: "/totally/unrelated/place", UpdatedAt: now},
		{ID: "exact", Cwd: cwd, UpdatedAt: now},
		{ID: "empty", Cwd: "", UpdatedAt: now},
	}
	SortConversations(convs, SortFieldScore, SortOrderDesc)

	if convs[0].ID != "exact" {
		t.Fatalf("expected exact cwd match first, got %q", convs[0].ID)
	}
}

// TestSortConversationsScoreTieFallsBackToDate exercises the score sort when two
// conversations share the same proximity: the more recent one must sort first
// under descending order because recency dominates the remaining age term.
func TestSortConversationsScoreTieFallsBackToDate(t *testing.T) {
	now := time.Now()

	convs := []Conversation{
		{ID: "old", Cwd: "", UpdatedAt: now.Add(-10 * time.Hour)},
		{ID: "new", Cwd: "", UpdatedAt: now.Add(-1 * time.Hour)},
	}
	SortConversations(convs, SortFieldScore, SortOrderDesc)

	if convs[0].ID != "new" {
		t.Fatalf("expected more recent conversation first, got %q", convs[0].ID)
	}
}
