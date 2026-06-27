package proxy

import (
	"fmt"
	"gpt-load/internal/models"
	"strings"
	"sync"
	"time"
)

type proxySelection struct {
	URL             string
	FromPool        bool
	CooldownSeconds int
}

type proxyPoolManager struct {
	mu          sync.Mutex
	affinity    map[string]string
	unavailable map[string]time.Time
}

func newProxyPoolManager() *proxyPoolManager {
	return &proxyPoolManager{
		affinity:    make(map[string]string),
		unavailable: make(map[string]time.Time),
	}
}

func (m *proxyPoolManager) Select(group *models.Group, keyID uint) (proxySelection, error) {
	if group == nil {
		return proxySelection{}, fmt.Errorf("group is nil")
	}

	groupConfig, err := models.DecodeGroupConfig(group.Config)
	if err != nil {
		return proxySelection{}, fmt.Errorf("failed to decode group config: %w", err)
	}
	if groupConfig.ProxyPool == nil || len(groupConfig.ProxyPool.Proxies) == 0 {
		return proxySelection{URL: group.EffectiveConfig.ProxyURL}, nil
	}

	cooldownSeconds := groupConfig.ProxyPool.CooldownSeconds
	if cooldownSeconds <= 0 {
		cooldownSeconds = 60
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	groupPrefix := fmt.Sprintf("%d:", group.ID)
	available := make([]string, 0, len(groupConfig.ProxyPool.Proxies))
	currentProxySet := make(map[string]struct{}, len(groupConfig.ProxyPool.Proxies))
	for _, proxyURL := range groupConfig.ProxyPool.Proxies {
		proxyURL = strings.TrimSpace(proxyURL)
		if proxyURL == "" {
			continue
		}
		currentProxySet[proxyURL] = struct{}{}
		if until, blocked := m.unavailable[proxyStateKey(group.ID, proxyURL)]; blocked {
			if now.Before(until) {
				continue
			}
			delete(m.unavailable, proxyStateKey(group.ID, proxyURL))
		}
		available = append(available, proxyURL)
	}

	if len(available) == 0 {
		return proxySelection{}, fmt.Errorf("no available proxy in group proxy pool")
	}

	affinityKey := fmt.Sprintf("%d:%d", group.ID, keyID)
	if assigned, exists := m.affinity[affinityKey]; exists {
		if _, stillConfigured := currentProxySet[assigned]; !stillConfigured {
			delete(m.affinity, affinityKey)
		} else if isInStringSlice(available, assigned) {
			return proxySelection{URL: assigned, FromPool: true, CooldownSeconds: cooldownSeconds}, nil
		}
	}

	counts := make(map[string]int, len(available))
	for _, proxyURL := range available {
		counts[proxyURL] = 0
	}
	for key, proxyURL := range m.affinity {
		if !strings.HasPrefix(key, groupPrefix) {
			continue
		}
		if _, ok := counts[proxyURL]; ok {
			counts[proxyURL]++
		}
	}

	selected := available[0]
	for _, proxyURL := range available[1:] {
		if counts[proxyURL] < counts[selected] {
			selected = proxyURL
		}
	}
	m.affinity[affinityKey] = selected

	return proxySelection{URL: selected, FromPool: true, CooldownSeconds: cooldownSeconds}, nil
}

func (m *proxyPoolManager) MarkFailure(groupID, keyID uint, proxyURL string, cooldownSeconds int) {
	if strings.TrimSpace(proxyURL) == "" {
		return
	}
	if cooldownSeconds <= 0 {
		cooldownSeconds = 60
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.unavailable[proxyStateKey(groupID, proxyURL)] = time.Now().Add(time.Duration(cooldownSeconds) * time.Second)
	affinityKey := fmt.Sprintf("%d:%d", groupID, keyID)
	if m.affinity[affinityKey] == proxyURL {
		delete(m.affinity, affinityKey)
	}
}

func (m *proxyPoolManager) MarkSuccess(groupID uint, proxyURL string) {
	if strings.TrimSpace(proxyURL) == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.unavailable, proxyStateKey(groupID, proxyURL))
}

func (m *proxyPoolManager) RemoveProxyAffinity(groupID uint, keyIDs []uint) {
	if len(keyIDs) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, keyID := range keyIDs {
		delete(m.affinity, fmt.Sprintf("%d:%d", groupID, keyID))
	}
}

func (m *proxyPoolManager) ClearProxyAffinity(groupID uint) {
	m.mu.Lock()
	defer m.mu.Unlock()

	prefix := fmt.Sprintf("%d:", groupID)
	for key := range m.affinity {
		if strings.HasPrefix(key, prefix) {
			delete(m.affinity, key)
		}
	}
	for key := range m.unavailable {
		if strings.HasPrefix(key, prefix) {
			delete(m.unavailable, key)
		}
	}
}

func proxyStateKey(groupID uint, proxyURL string) string {
	return fmt.Sprintf("%d:%s", groupID, proxyURL)
}

func isInStringSlice(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
