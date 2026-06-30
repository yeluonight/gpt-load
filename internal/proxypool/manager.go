package proxypool

import (
	"fmt"
	"gpt-load/internal/models"
	"strings"
	"sync"
	"time"
)

type Selection struct {
	URL             string
	FromPool        bool
	CooldownSeconds int
}

type Manager struct {
	mu          sync.Mutex
	affinity    map[string]string
	unavailable map[string]time.Time
}

func NewManager() *Manager {
	return &Manager{
		affinity:    make(map[string]string),
		unavailable: make(map[string]time.Time),
	}
}

func (m *Manager) Select(group *models.Group, keyID uint) (Selection, error) {
	return m.SelectExcluding(group, keyID, nil)
}

func (m *Manager) SelectExcluding(group *models.Group, keyID uint, excluded map[string]struct{}) (Selection, error) {
	if group == nil {
		return Selection{}, fmt.Errorf("group is nil")
	}

	groupConfig, err := models.DecodeGroupConfig(group.Config)
	if err != nil {
		return Selection{}, fmt.Errorf("failed to decode group config: %w", err)
	}
	if groupConfig.ProxyPool == nil || len(groupConfig.ProxyPool.Entries()) == 0 {
		return Selection{URL: group.EffectiveConfig.ProxyURL}, nil
	}

	selectableEntries := groupConfig.ProxyPool.SelectableEntries()
	if len(selectableEntries) == 0 {
		return Selection{}, fmt.Errorf("no enabled proxy in group proxy pool")
	}

	cooldownSeconds := groupConfig.ProxyPool.AutoEnableIntervalSeconds
	if cooldownSeconds <= 0 {
		cooldownSeconds = groupConfig.ProxyPool.CooldownSeconds
		if cooldownSeconds <= 0 {
			cooldownSeconds = 60
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	groupPrefix := fmt.Sprintf("%d:", group.ID)
	available := make([]string, 0, len(selectableEntries))
	currentProxySet := make(map[string]struct{}, len(selectableEntries))
	cooling := make([]string, 0, len(selectableEntries))
	for _, entry := range selectableEntries {
		proxyURL := entry.URL
		currentProxySet[proxyURL] = struct{}{}
		if _, skipped := excluded[proxyURL]; skipped {
			continue
		}
		if until, blocked := m.unavailable[proxyStateKey(group.ID, proxyURL)]; blocked {
			if now.Before(until) {
				cooling = append(cooling, proxyURL)
				continue
			}
			delete(m.unavailable, proxyStateKey(group.ID, proxyURL))
		}
		available = append(available, proxyURL)
	}

	if len(available) == 0 {
		if len(cooling) == 0 {
			if len(excluded) > 0 {
				return Selection{}, fmt.Errorf("no untried available proxy in group proxy pool")
			}
			return Selection{}, fmt.Errorf("no available proxy in group proxy pool")
		}
		// If every enabled proxy is in temporary cooldown, probe one early instead
		// of failing the whole group. This avoids a single bad key/upstream incident
		// turning into a prolonged full-pool outage.
		available = append(available, cooling[0])
		delete(m.unavailable, proxyStateKey(group.ID, cooling[0]))
	}

	affinityKey := fmt.Sprintf("%d:%d", group.ID, keyID)
	if assigned, exists := m.affinity[affinityKey]; exists {
		if _, stillConfigured := currentProxySet[assigned]; !stillConfigured {
			delete(m.affinity, affinityKey)
		} else if isInStringSlice(available, assigned) {
			return Selection{URL: assigned, FromPool: true, CooldownSeconds: cooldownSeconds}, nil
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

	return Selection{URL: selected, FromPool: true, CooldownSeconds: cooldownSeconds}, nil
}

func (m *Manager) MarkFailure(groupID, keyID uint, proxyURL string, cooldownSeconds int) {
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

func (m *Manager) MarkSuccess(groupID uint, proxyURL string) {
	if strings.TrimSpace(proxyURL) == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.unavailable, proxyStateKey(groupID, proxyURL))
}

func (m *Manager) RemoveProxyAffinity(groupID uint, keyIDs []uint) {
	if len(keyIDs) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, keyID := range keyIDs {
		delete(m.affinity, fmt.Sprintf("%d:%d", groupID, keyID))
	}
}

func (m *Manager) ClearProxyAffinity(groupID uint) {
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
