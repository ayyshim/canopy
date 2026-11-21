package api

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// previewGrant is what a preview token lets its holder read.
type previewGrant struct {
	UserID      uuid.UUID  `json:"user_id"`
	WorkspaceID uuid.UUID  `json:"workspace_id"`
	OutputID    uuid.UUID  `json:"output_id"`
	TaskID      *uuid.UUID `json:"task_id,omitempty"`
	GoalID      *uuid.UUID `json:"goal_id,omitempty"`
	ExpiresAt   time.Time  `json:"expires_at"`
}

// PreviewStore keeps preview tokens until they expire.
type PreviewStore interface {
	Put(ctx context.Context, token string, g previewGrant, ttl time.Duration) error
	Get(ctx context.Context, token string) (previewGrant, bool, error)
}

// MemoryPreviews is a single-process store, used when Redis is not set up.
type MemoryPreviews struct {
	mu sync.Mutex
	m  map[string]previewGrant
}

func (s *MemoryPreviews) Put(_ context.Context, token string, g previewGrant, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]previewGrant{}
	}
	now := time.Now()
	for k, v := range s.m {
		if now.After(v.ExpiresAt) {
			delete(s.m, k)
		}
	}
	s.m[token] = g
	return nil
}

func (s *MemoryPreviews) Get(_ context.Context, token string) (previewGrant, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.m[token]
	return g, ok, nil
}

// RedisPreviews shares tokens across hub instances.
type RedisPreviews struct{ Client *redis.Client }

func (s RedisPreviews) Put(ctx context.Context, token string, g previewGrant, ttl time.Duration) error {
	b, err := json.Marshal(g)
	if err != nil {
		return err
	}
	return s.Client.Set(ctx, "preview:"+token, b, ttl).Err()
}

func (s RedisPreviews) Get(ctx context.Context, token string) (previewGrant, bool, error) {
	var g previewGrant
	b, err := s.Client.Get(ctx, "preview:"+token).Bytes()
	if errors.Is(err, redis.Nil) {
		return g, false, nil
	} else if err != nil {
		return g, false, err
	}
	return g, true, json.Unmarshal(b, &g)
}
