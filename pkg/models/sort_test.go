package models

import (
	"os"
	"testing"
	"time"
)

func TestSortConversations(t *testing.T) {
	now := time.Now()
	cwd, _ := os.Getwd()

	convs := []Conversation{
		{
			ID:        "1",
			Title:     "A",
			Cwd:       cwd, // closest
			UpdatedAt: now.Add(-10 * time.Minute),
		},
		{
			ID:        "2",
			Title:     "B",
			Cwd:       "/different/path/entirely", // far
			UpdatedAt: now.Add(-1 * time.Minute),
		},
		{
			ID:        "3",
			Title:     "C",
			Cwd:       "",
			FilePath:  "/another/random/file", // no Cwd, fall back to FilePath
			UpdatedAt: now.Add(-60 * time.Minute),
		},
	}

	// Test case helper function
	checkOrder := func(t *testing.T, sorted []Conversation, expectedIDs []string) {
		if len(sorted) != len(expectedIDs) {
			t.Fatalf("expected %d items, got %d", len(expectedIDs), len(sorted))
		}
		for i, id := range expectedIDs {
			if sorted[i].ID != id {
				t.Errorf("at index %d: expected ID %s, got %s", i, id, sorted[i].ID)
			}
		}
	}

	t.Run("Sort by Date Descending (default/standard)", func(t *testing.T) {
		testConvs := make([]Conversation, len(convs))
		copy(testConvs, convs)
		SortConversations(testConvs, SortFieldDate, SortOrderDesc)
		// Expected: 2 (1 min ago), 1 (10 mins ago), 3 (60 mins ago)
		checkOrder(t, testConvs, []string{"2", "1", "3"})
	})

	t.Run("Sort by Date Ascending", func(t *testing.T) {
		testConvs := make([]Conversation, len(convs))
		copy(testConvs, convs)
		SortConversations(testConvs, SortFieldDate, SortOrderAsc)
		// Expected: 3, 1, 2
		checkOrder(t, testConvs, []string{"3", "1", "2"})
	})

	t.Run("Sort by Path Ascending", func(t *testing.T) {
		testConvs := make([]Conversation, len(convs))
		copy(testConvs, convs)
		SortConversations(testConvs, SortFieldPath, SortOrderAsc)
		// /another/random/file (ID 3), /different/path/entirely (ID 2), cwd (ID 1)
		// Note: since cwd starts with /home/zadkiel (or similar depending on environment),
		// let's check string sorting order.
		// /another/random/file is lower than /different/path/entirely.
		// /home/... is between or after depending on cwd.
		// Let's explicitly set paths to be predictable.
	})
}

func TestSortConversationsWithExplicitPaths(t *testing.T) {
	now := time.Now()

	convs := []Conversation{
		{
			ID:        "1",
			Cwd:       "/z/path",
			UpdatedAt: now.Add(-10 * time.Minute),
		},
		{
			ID:        "2",
			Cwd:       "/a/path",
			UpdatedAt: now.Add(-1 * time.Minute),
		},
		{
			ID:        "3",
			FilePath:  "/m/path", // fallback path
			UpdatedAt: now.Add(-60 * time.Minute),
		},
	}

	checkOrder := func(t *testing.T, sorted []Conversation, expectedIDs []string) {
		for i, id := range expectedIDs {
			if sorted[i].ID != id {
				t.Errorf("expected ID %s at index %d, got %s", id, i, sorted[i].ID)
			}
		}
	}

	t.Run("Path Ascending", func(t *testing.T) {
		testConvs := make([]Conversation, len(convs))
		copy(testConvs, convs)
		SortConversations(testConvs, SortFieldPath, SortOrderAsc)
		// Expected: 2 (/a/path), 3 (/m/path), 1 (/z/path)
		checkOrder(t, testConvs, []string{"2", "3", "1"})
	})

	t.Run("Path Descending", func(t *testing.T) {
		testConvs := make([]Conversation, len(convs))
		copy(testConvs, convs)
		SortConversations(testConvs, SortFieldPath, SortOrderDesc)
		// Expected: 1 (/z/path), 3 (/m/path), 2 (/a/path)
		checkOrder(t, testConvs, []string{"1", "3", "2"})
	})
}
