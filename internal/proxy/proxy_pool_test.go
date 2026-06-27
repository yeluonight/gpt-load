package proxy

import (
	"gpt-load/internal/models"
	"testing"
	"time"

	"gorm.io/datatypes"
)

func TestProxyPoolKeepsKeyAffinityAndDistributesKeys(t *testing.T) {
	manager := newProxyPoolManager()
	group := &models.Group{
		ID: 1,
		Config: datatypes.JSONMap{
			"proxy_pool": map[string]any{
				"proxies": []any{
					"http://127.0.0.1:1001",
					"http://127.0.0.1:1002",
				},
			},
		},
	}

	key1First, err := manager.Select(group, 1)
	if err != nil {
		t.Fatalf("select key1 first: %v", err)
	}
	key2, err := manager.Select(group, 2)
	if err != nil {
		t.Fatalf("select key2: %v", err)
	}
	key1Second, err := manager.Select(group, 1)
	if err != nil {
		t.Fatalf("select key1 second: %v", err)
	}

	if key1First.URL != key1Second.URL {
		t.Fatalf("key affinity changed from %s to %s", key1First.URL, key1Second.URL)
	}
	if key1First.URL == key2.URL {
		t.Fatalf("expected key2 to use a different proxy when available, got %s", key2.URL)
	}
}

func TestProxyPoolFailureSwitchesProxy(t *testing.T) {
	manager := newProxyPoolManager()
	group := &models.Group{
		ID: 1,
		Config: datatypes.JSONMap{
			"proxy_pool": map[string]any{
				"proxies": []any{
					"http://127.0.0.1:1001",
					"http://127.0.0.1:1002",
				},
				"cooldown_seconds": 60,
			},
		},
	}

	first, err := manager.Select(group, 1)
	if err != nil {
		t.Fatalf("select first proxy: %v", err)
	}
	manager.MarkFailure(group.ID, 1, first.URL, first.CooldownSeconds)

	next, err := manager.Select(group, 1)
	if err != nil {
		t.Fatalf("select after failure: %v", err)
	}
	if next.URL == first.URL {
		t.Fatalf("proxy did not switch after failure: %s", next.URL)
	}
}

func TestProxyPoolCleansAffinity(t *testing.T) {
	manager := newProxyPoolManager()
	manager.affinity["1:10"] = "http://127.0.0.1:1001"
	manager.affinity["1:11"] = "http://127.0.0.1:1002"
	manager.affinity["2:10"] = "http://127.0.0.1:1001"
	manager.unavailable[proxyStateKey(1, "http://127.0.0.1:1001")] = time.Now().Add(time.Minute)

	manager.RemoveProxyAffinity(1, []uint{10})
	if _, exists := manager.affinity["1:10"]; exists {
		t.Fatal("RemoveProxyAffinity did not remove selected key affinity")
	}
	if _, exists := manager.affinity["1:11"]; !exists {
		t.Fatal("RemoveProxyAffinity removed unrelated key affinity")
	}

	manager.ClearProxyAffinity(1)
	if _, exists := manager.affinity["1:11"]; exists {
		t.Fatal("ClearProxyAffinity did not remove group affinity")
	}
	if _, exists := manager.affinity["2:10"]; !exists {
		t.Fatal("ClearProxyAffinity removed another group's affinity")
	}
	if _, exists := manager.unavailable[proxyStateKey(1, "http://127.0.0.1:1001")]; exists {
		t.Fatal("ClearProxyAffinity did not remove group unavailable state")
	}
}
