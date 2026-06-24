package models

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SortField defines the criteria for sorting conversations.
type SortField string

const (
	SortFieldDate  SortField = "date"
	SortFieldPath  SortField = "path"
	SortFieldScore SortField = "score"
)

// SortOrder defines the direction of the sort.
type SortOrder string

const (
	SortOrderAsc  SortOrder = "asc"
	SortOrderDesc SortOrder = "desc"
)

// SortConversations sorts the slice of conversations in-place by the specified field and order.
func SortConversations(convs []Conversation, sortBy SortField, order SortOrder) {
	currentCwd, _ := os.Getwd()
	sort.Slice(convs, func(i, j int) bool {
		switch sortBy {
		case SortFieldDate:
			if order == SortOrderDesc {
				return convs[i].UpdatedAt.After(convs[j].UpdatedAt)
			}
			return convs[i].UpdatedAt.Before(convs[j].UpdatedAt)
		case SortFieldPath:
			pathI := convs[i].Cwd
			if pathI == "" {
				pathI = convs[i].FilePath
			}
			pathJ := convs[j].Cwd
			if pathJ == "" {
				pathJ = convs[j].FilePath
			}
			if order == SortOrderDesc {
				return strings.ToLower(pathI) > strings.ToLower(pathJ)
			}
			return strings.ToLower(pathI) < strings.ToLower(pathJ)
		case SortFieldScore:
			scoreI := ComputeSortScore(convs[i], currentCwd)
			scoreJ := ComputeSortScore(convs[j], currentCwd)
			if scoreI == scoreJ {
				// Fallback to date descending if scores are identical
				return convs[i].UpdatedAt.After(convs[j].UpdatedAt)
			}
			if order == SortOrderDesc {
				return scoreI > scoreJ
			}
			return scoreI < scoreJ
		default:
			// Default to date descending
			if order == SortOrderAsc {
				return convs[i].UpdatedAt.Before(convs[j].UpdatedAt)
			}
			return convs[i].UpdatedAt.After(convs[j].UpdatedAt)
		}
	})
}

func cwdScore(current, target string) int {
	if target == "" {
		return -1
	}
	current = filepath.Clean(current)
	target = filepath.Clean(target)

	if current == target {
		return 10000
	}

	currParts := strings.Split(current, string(filepath.Separator))
	targParts := strings.Split(target, string(filepath.Separator))

	commonLen := 0
	for i := 0; i < len(currParts) && i < len(targParts); i++ {
		if currParts[i] == targParts[i] {
			commonLen += len(currParts[i]) + 1
		} else {
			break
		}
	}

	return (commonLen * 100) - len(target)
}

// ComputeSortScore calculates a score based on directory proximity and age.
func ComputeSortScore(c Conversation, currentCwd string) float64 {
	prox := float64(cwdScore(currentCwd, c.Cwd))
	if prox < 0 {
		prox = 0
	}

	bonusDays := prox / 2000.0
	actualAgeDays := time.Since(c.UpdatedAt).Hours() / 24.0

	return bonusDays - actualAgeDays
}
