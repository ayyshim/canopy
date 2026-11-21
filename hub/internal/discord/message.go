// Package discord sends messages to Discord webhooks.
package discord

import "time"

type Message struct {
	Username   string
	AvatarURL  string
	Content    string
	Embeds     []Embed
	Files      []File
	Buttons    []Button
	ThreadName string
	ThreadID   string
	// RoleID is the only role that may be pinged. It is added to content if missing.
	RoleID string
}

type Embed struct {
	Title       string
	Description string
	URL         string
	Color       int
	Fields      []Field
	// Image is a URL or "attachment://name" for a file in Message.Files.
	Image     string
	Footer    string
	Timestamp time.Time
}

type Field struct {
	Name   string
	Value  string
	Inline bool
}

type File struct {
	Name        string
	ContentType string
	Data        []byte
	// URL is used instead of the file when it is skipped.
	URL string
}

type Button struct {
	Label string
	URL   string
}

type Result struct {
	MessageID string
	ChannelID string
}
