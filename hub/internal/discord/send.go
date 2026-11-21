package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

var (
	// ErrWebhookGone means the webhook was deleted or its token is invalid.
	ErrWebhookGone = errors.New("discord: webhook gone")
	ErrInvalidURL  = errors.New("discord: invalid webhook url")
	ErrEmpty       = errors.New("discord: message is empty")
)

// StatusError is a non-success reply from Discord.
type StatusError struct {
	Status    int
	Message   string
	Retryable bool
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("discord: status %d: %s", e.Status, e.Message)
}

// IsRetryable reports whether trying again later may work.
func IsRetryable(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Retryable
	}
	var te *transportError
	return errors.As(err, &te)
}

// transportError hides the request URL, which holds the webhook token.
type transportError struct{ err error }

func (e *transportError) Error() string { return "discord: request failed: " + e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

type Client struct {
	HTTP     *http.Client
	Attempts int           // total tries, default 3
	MaxWait  time.Duration // cap on any single wait, default 10s
	Backoff  time.Duration // base wait after a 5xx, default 1s
}

var defaultClient = &Client{}

func Send(ctx context.Context, webhookURL string, m Message) (Result, error) {
	return defaultClient.Send(ctx, webhookURL, m)
}

func (c *Client) Send(ctx context.Context, webhookURL string, m Message) (Result, error) {
	endpoint, err := endpointURL(webhookURL, m)
	if err != nil {
		return Result{}, err
	}
	if m.Content == "" && len(m.Embeds) == 0 && len(m.Files) == 0 {
		return Result{}, ErrEmpty
	}
	withButtons := len(m.Buttons) > 0
	for {
		res, err := c.post(ctx, endpoint, prepare(m, withButtons), withButtons)
		var se *StatusError
		if withButtons && errors.As(err, &se) && se.Status == http.StatusBadRequest {
			withButtons = false
			continue
		}
		return res, err
	}
}

func endpointURL(raw string, m Message) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", ErrInvalidURL
	}
	q := u.Query()
	q.Set("wait", "true")
	if m.ThreadID != "" {
		q.Set("thread_id", m.ThreadID)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (c *Client) post(ctx context.Context, endpoint string, p prepared, buttons bool) (Result, error) {
	body, ctype, err := p.encode()
	if err != nil {
		return Result{}, err
	}
	if buttons && len(p.payload.Components) > 0 {
		u, _ := url.Parse(endpoint)
		q := u.Query()
		q.Set("with_components", "true")
		u.RawQuery = q.Encode()
		endpoint = u.String()
	}
	attempts, maxWait, backoff := c.Attempts, c.MaxWait, c.Backoff
	if attempts <= 0 {
		attempts = 3
	}
	if maxWait <= 0 {
		maxWait = 10 * time.Second
	}
	if backoff <= 0 {
		backoff = time.Second
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	for i := 1; ; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return Result{}, ErrInvalidURL
		}
		req.Header.Set("Content-Type", ctype)
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err
			}
			if i >= attempts {
				return Result{}, &transportError{err}
			}
			if err := sleep(ctx, min(backoff*time.Duration(i), maxWait)); err != nil {
				return Result{}, err
			}
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			var r struct {
				ID        string `json:"id"`
				ChannelID string `json:"channel_id"`
			}
			json.Unmarshal(data, &r)
			return Result{MessageID: r.ID, ChannelID: r.ChannelID}, nil
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound:
			return Result{}, fmt.Errorf("%w (status %d)", ErrWebhookGone, resp.StatusCode)
		case resp.StatusCode == http.StatusTooManyRequests:
			if i >= attempts {
				return Result{}, statusErr(resp.StatusCode, data, true)
			}
			if err := sleep(ctx, min(retryAfter(resp, data), maxWait)); err != nil {
				return Result{}, err
			}
		case resp.StatusCode >= 500:
			if i >= attempts {
				return Result{}, statusErr(resp.StatusCode, data, true)
			}
			if err := sleep(ctx, min(backoff*time.Duration(i), maxWait)); err != nil {
				return Result{}, err
			}
		default:
			return Result{}, statusErr(resp.StatusCode, data, false)
		}
	}
}

func statusErr(status int, data []byte, retryable bool) *StatusError {
	var e struct {
		Message string `json:"message"`
	}
	json.Unmarshal(data, &e)
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	return &StatusError{Status: status, Message: truncate(e.Message, 200), Retryable: retryable}
}

func retryAfter(resp *http.Response, data []byte) time.Duration {
	var b struct {
		RetryAfter float64 `json:"retry_after"`
	}
	if json.Unmarshal(data, &b) == nil && b.RetryAfter > 0 {
		return time.Duration(b.RetryAfter * float64(time.Second))
	}
	if s, err := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64); err == nil && s > 0 {
		return time.Duration(s * float64(time.Second))
	}
	return time.Second
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
