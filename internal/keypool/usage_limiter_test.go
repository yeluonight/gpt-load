package keypool

import (
	"errors"
	"gpt-load/internal/encryption"
	"gpt-load/internal/models"
	"gpt-load/internal/store"
	"strconv"
	"testing"
	"time"

	"gorm.io/datatypes"
)

func TestSelectKeyForRequestSkipsLimitedKey(t *testing.T) {
	memoryStore := store.NewMemoryStore()
	encryptionSvc, err := encryption.NewService("")
	if err != nil {
		t.Fatalf("failed to create encryption service: %v", err)
	}
	provider := &KeyProvider{
		store:         memoryStore,
		encryptionSvc: encryptionSvc,
	}

	for _, keyID := range []uint{1, 2} {
		if err := memoryStore.HSet(keyHashKey(keyID), map[string]any{
			"id":            keyID,
			"key_string":    "sk-test",
			"status":        models.KeyStatusActive,
			"failure_count": 0,
			"group_id":      1,
			"created_at":    time.Now().Unix(),
		}); err != nil {
			t.Fatalf("failed to seed key %d: %v", keyID, err)
		}
	}
	if err := memoryStore.LPush("group:1:active_keys", 1, 2); err != nil {
		t.Fatalf("failed to seed active key list: %v", err)
	}

	group := &models.Group{
		ID: 1,
		Config: datatypes.JSONMap{
			"model_rate_limits": []any{
				map[string]any{"model": "gpt-test", "rpm": 1},
			},
		},
	}

	firstKey, _, err := provider.SelectKeyForRequest(group, "gpt-test", 1)
	if err != nil {
		t.Fatalf("first select returned error: %v", err)
	}
	secondKey, _, err := provider.SelectKeyForRequest(group, "gpt-test", 1)
	if err != nil {
		t.Fatalf("second select returned error: %v", err)
	}
	if firstKey.ID == secondKey.ID {
		t.Fatalf("second select used key %d again; want a different key after rpm limit", secondKey.ID)
	}

	_, _, err = provider.SelectKeyForRequest(group, "gpt-test", 1)
	if !IsLimitExceeded(err) {
		t.Fatalf("third select error = %v, want LimitExceededError", err)
	}
}

func TestUsageReservationReleaseRollsBackCounters(t *testing.T) {
	memoryStore := store.NewMemoryStore()
	provider := &KeyProvider{store: memoryStore}
	reservation := &UsageReservation{
		provider: provider,
		counters: []usageCounter{
			{key: "usage:test", incr: 1},
		},
	}

	if _, allowed, err := memoryStore.TryIncrByWithTTL("usage:test", 1, 1, time.Minute); err != nil || !allowed {
		t.Fatalf("failed to seed usage counter: allowed=%v err=%v", allowed, err)
	}
	reservation.Release()

	_, allowed, err := memoryStore.TryIncrByWithTTL("usage:test", 1, 1, time.Minute)
	if err != nil {
		t.Fatalf("TryIncrByWithTTL returned error: %v", err)
	}
	if !allowed {
		t.Fatal("counter was not released")
	}
}

func TestIsLimitExceeded(t *testing.T) {
	if !IsLimitExceeded(&LimitExceededError{Reason: "limited"}) {
		t.Fatal("LimitExceededError was not detected")
	}
	if IsLimitExceeded(errors.New("other")) {
		t.Fatal("non-limit error detected as limit error")
	}
}

func keyHashKey(keyID uint) string {
	return "key:" + strconv.FormatUint(uint64(keyID), 10)
}
