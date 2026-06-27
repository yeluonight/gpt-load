package store

import (
	"testing"
	"time"
)

func TestMemoryStoreTryIncrByWithTTL(t *testing.T) {
	s := NewMemoryStore()

	current, allowed, err := s.TryIncrByWithTTL("counter", 2, 3, time.Minute)
	if err != nil {
		t.Fatalf("TryIncrByWithTTL returned error: %v", err)
	}
	if !allowed || current != 2 {
		t.Fatalf("first increment = (%d, %v), want (2, true)", current, allowed)
	}

	current, allowed, err = s.TryIncrByWithTTL("counter", 2, 3, time.Minute)
	if err != nil {
		t.Fatalf("TryIncrByWithTTL returned error: %v", err)
	}
	if allowed || current != 2 {
		t.Fatalf("limited increment = (%d, %v), want (2, false)", current, allowed)
	}

	current, err = s.IncrBy("counter", -1)
	if err != nil {
		t.Fatalf("IncrBy returned error: %v", err)
	}
	if current != 1 {
		t.Fatalf("counter after rollback = %d, want 1", current)
	}
}
