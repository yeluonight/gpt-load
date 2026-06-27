package proxypool

import (
	"fmt"
	"gpt-load/internal/models"
	"testing"
	"time"

	"gorm.io/datatypes"
)

func TestProxyPoolKeepsKeyAffinityAndDistributesKeys(t *testing.T) {
	manager := NewManager()
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
	manager := NewManager()
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

func TestProxyPoolOverridesGroupProxyURL(t *testing.T) {
	manager := NewManager()
	group := &models.Group{
		ID: 1,
		Config: datatypes.JSONMap{
			"proxy_pool": map[string]any{
				"proxies": []any{"http://127.0.0.1:1001"},
			},
		},
	}
	group.EffectiveConfig.ProxyURL = "http://127.0.0.1:7890"

	selection, err := manager.Select(group, 1)
	if err != nil {
		t.Fatalf("select proxy: %v", err)
	}
	if !selection.FromPool {
		t.Fatal("expected proxy_pool selection to be marked FromPool")
	}
	if selection.URL != "http://127.0.0.1:1001" {
		t.Fatalf("expected proxy_pool URL, got %s", selection.URL)
	}
}

func TestProxyPoolFallsBackToGroupProxyURLWhenPoolUnset(t *testing.T) {
	manager := NewManager()
	group := &models.Group{ID: 1}
	group.EffectiveConfig.ProxyURL = "http://127.0.0.1:7890"

	selection, err := manager.Select(group, 1)
	if err != nil {
		t.Fatalf("select fallback proxy: %v", err)
	}
	if selection.FromPool {
		t.Fatal("expected fallback proxy_url selection not to be marked FromPool")
	}
	if selection.URL != "http://127.0.0.1:7890" {
		t.Fatalf("expected fallback proxy_url, got %s", selection.URL)
	}
}

func TestProxyPoolSkipsManualDisabledItems(t *testing.T) {
	manager := NewManager()
	group := &models.Group{
		ID: 1,
		Config: datatypes.JSONMap{
			"proxy_pool": map[string]any{
				"items": []any{
					map[string]any{"url": "http://127.0.0.1:1001", "disabled": true},
					map[string]any{"url": "http://127.0.0.1:1002"},
				},
			},
		},
	}

	selection, err := manager.Select(group, 1)
	if err != nil {
		t.Fatalf("select proxy: %v", err)
	}
	if selection.URL != "http://127.0.0.1:1002" {
		t.Fatalf("expected enabled proxy, got %s", selection.URL)
	}
}

func TestProxyPoolReprobesWhenAllEnabledProxiesAreCooling(t *testing.T) {
	manager := NewManager()
	group := &models.Group{
		ID: 1,
		Config: datatypes.JSONMap{
			"proxy_pool": map[string]any{
				"proxies": []any{
					"http://127.0.0.1:1001",
					"http://127.0.0.1:1002",
				},
				"auto_enable_interval_seconds": 600,
			},
		},
	}

	first, err := manager.Select(group, 1)
	if err != nil {
		t.Fatalf("select first proxy: %v", err)
	}
	manager.MarkFailure(group.ID, 1, first.URL, first.CooldownSeconds)
	second, err := manager.Select(group, 2)
	if err != nil {
		t.Fatalf("select second proxy: %v", err)
	}
	manager.MarkFailure(group.ID, 2, second.URL, second.CooldownSeconds)

	selection, err := manager.Select(group, 3)
	if err != nil {
		t.Fatalf("expected early reprobe instead of full pool failure: %v", err)
	}
	if selection.URL == "" {
		t.Fatal("expected selected proxy")
	}
}

func TestProxyTransportErrorRequiresProxyEvidence(t *testing.T) {
	proxyURL := "http://127.0.0.1:1001"
	if IsProxyTransportError(fmt.Errorf("context deadline exceeded while awaiting headers"), proxyURL) {
		t.Fatal("generic upstream timeout should not be classified as proxy transport error")
	}
	if !IsProxyTransportError(fmt.Errorf("proxyconnect tcp: dial tcp 127.0.0.1:1001: connect: connection refused"), proxyURL) {
		t.Fatal("proxyconnect failure should be classified as proxy transport error")
	}
}

func TestProxyPoolCleansAffinity(t *testing.T) {
	manager := NewManager()
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
