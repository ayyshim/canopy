// Package mail sends transactional email over SMTP.
package mail

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
	"sync"
	"time"
)

type Message struct {
	To      string
	Subject string
	Text    string
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

// SMTP sends through a plain SMTP relay (Mailpit in development).
type SMTP struct {
	Addr string
	From string
}

func (s SMTP) Send(_ context.Context, m Message) error {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", s.From)
	fmt.Fprintf(&b, "To: %s\r\n", m.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", m.Subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(strings.ReplaceAll(m.Text, "\n", "\r\n"))
	return smtp.SendMail(s.Addr, nil, envelopeAddr(s.From), []string{m.To}, []byte(b.String()))
}

func envelopeAddr(from string) string {
	if i := strings.Index(from, "<"); i >= 0 {
		return strings.TrimSuffix(from[i+1:], ">")
	}
	return from
}

// Log writes messages to the log instead of sending them.
type Log struct{}

func (Log) Send(ctx context.Context, m Message) error {
	slog.InfoContext(ctx, "mail (not sent)", "to", m.To, "subject", m.Subject, "text", m.Text)
	return nil
}

// Memory keeps messages for tests.
type Memory struct {
	mu   sync.Mutex
	Sent []Message
}

func (m *Memory) Send(_ context.Context, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sent = append(m.Sent, msg)
	return nil
}

func (m *Memory) Last() (Message, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Sent) == 0 {
		return Message{}, false
	}
	return m.Sent[len(m.Sent)-1], true
}
