package models

import "time"

// Conversation represents a normalized session or history from an AI provider.
type Conversation struct {
	ID            string
	Provider      string
	FilePath      string
	UpdatedAt     time.Time
	Title         string
	Snippet       string
	Cwd           string
	ResumeID      string
	Messages      []Message
	Raw           []byte `json:"-"`
	SearchContent string `json:"-"`
}

// Message represents a single turn in a conversation.
type Message struct {
	Role      string // "user", "assistant", "tool"
	Content   string
	IsThought bool
}
