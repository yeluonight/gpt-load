package keypool

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"gpt-load/internal/models"
	"strings"
	"time"
	_ "time/tzdata"
)

var pacificLocation = loadPacificLocation()

type usageCounter struct {
	key  string
	incr int64
}

// UsageReservation tracks usage counters reserved before an upstream request.
type UsageReservation struct {
	provider *KeyProvider
	counters []usageCounter
}

// Release rolls back a reservation when the request did not reach upstream.
func (r *UsageReservation) Release() {
	if r == nil || r.provider == nil {
		return
	}
	for _, counter := range r.counters {
		if counter.incr <= 0 {
			continue
		}
		_, _ = r.provider.store.IncrBy(counter.key, -counter.incr)
	}
}

type LimitExceededError struct {
	Reason string
}

func (e *LimitExceededError) Error() string {
	if e == nil {
		return ""
	}
	return e.Reason
}

func IsLimitExceeded(err error) bool {
	var limitErr *LimitExceededError
	return errors.As(err, &limitErr)
}

// SelectKeyForRequest selects the next key that can accept this request and
// reserves configured per-key usage counters for it.
func (p *KeyProvider) SelectKeyForRequest(group *models.Group, model string, tokenEstimate int64) (*models.APIKey, *UsageReservation, error) {
	if group == nil {
		return nil, nil, fmt.Errorf("group is nil")
	}

	activeKeysListKey := fmt.Sprintf("group:%d:active_keys", group.ID)
	keyCount, err := p.store.LLen(activeKeysListKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get active key count: %w", err)
	}
	if keyCount <= 0 {
		return nil, nil, appErrNoActiveKeys()
	}

	var lastLimitErr error
	for i := int64(0); i < keyCount; i++ {
		apiKey, err := p.selectKey(group.ID)
		if err != nil {
			return nil, nil, err
		}

		reservation, err := p.reserveUsage(group, apiKey, model, tokenEstimate)
		if err == nil {
			return apiKey, reservation, nil
		}
		if IsLimitExceeded(err) {
			lastLimitErr = err
			continue
		}
		return nil, nil, err
	}

	if lastLimitErr != nil {
		return nil, nil, lastLimitErr
	}
	return nil, nil, appErrNoActiveKeys()
}

func appErrNoActiveKeys() error {
	return fmt.Errorf("no active API keys available for this group")
}

func (p *KeyProvider) reserveUsage(group *models.Group, apiKey *models.APIKey, model string, tokenEstimate int64) (*UsageReservation, error) {
	groupConfig, err := models.DecodeGroupConfig(group.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to decode group config: %w", err)
	}

	now := time.Now()
	reservation := &UsageReservation{provider: p}
	if groupConfig.KeyRequestLimit != nil && groupConfig.KeyRequestLimit.MaxRequests > 0 {
		counterKey, ttl, err := keyRequestLimitCounter(group.ID, apiKey.ID, groupConfig.KeyRequestLimit, now)
		if err != nil {
			return nil, err
		}
		if err := p.tryReserveCounter(reservation, counterKey, 1, groupConfig.KeyRequestLimit.MaxRequests, ttl, "key request limit reached"); err != nil {
			return nil, err
		}
	}

	if limit, ok := findModelRateLimit(groupConfig.ModelRateLimits, model); ok {
		ttl := minuteTTL(now)
		modelKey := modelCounterToken(model)
		minuteBucket := now.Unix() / 60
		if limit.RPM > 0 {
			counterKey := fmt.Sprintf("usage:group:%d:key:%d:model:%s:rpm:%d", group.ID, apiKey.ID, modelKey, minuteBucket)
			if err := p.tryReserveCounter(reservation, counterKey, 1, limit.RPM, ttl, "model rpm limit reached"); err != nil {
				return nil, err
			}
		}
		if limit.TPM > 0 {
			if tokenEstimate <= 0 {
				tokenEstimate = 1
			}
			counterKey := fmt.Sprintf("usage:group:%d:key:%d:model:%s:tpm:%d", group.ID, apiKey.ID, modelKey, minuteBucket)
			if err := p.tryReserveCounter(reservation, counterKey, tokenEstimate, limit.TPM, ttl, "model tpm limit reached"); err != nil {
				return nil, err
			}
		}
		if limit.RequestLimit != nil && limit.RequestLimit.MaxRequests > 0 {
			counterKey, ttl, err := modelRequestLimitCounter(group.ID, apiKey.ID, modelKey, limit.RequestLimit, now)
			if err != nil {
				return nil, err
			}
			if err := p.tryReserveCounter(reservation, counterKey, 1, limit.RequestLimit.MaxRequests, ttl, "model request limit reached"); err != nil {
				return nil, err
			}
		}
	}

	return reservation, nil
}

func (p *KeyProvider) tryReserveCounter(reservation *UsageReservation, key string, incr, limit int64, ttl time.Duration, reason string) error {
	_, allowed, err := p.store.TryIncrByWithTTL(key, incr, limit, ttl)
	if err != nil {
		reservation.Release()
		return err
	}
	if !allowed {
		reservation.Release()
		return &LimitExceededError{Reason: reason}
	}

	reservation.counters = append(reservation.counters, usageCounter{key: key, incr: incr})
	return nil
}

func findModelRateLimit(limits []models.ModelRateLimitConfig, model string) (models.ModelRateLimitConfig, bool) {
	model = strings.TrimSpace(model)
	var wildcard *models.ModelRateLimitConfig
	for i := range limits {
		limit := limits[i]
		if strings.TrimSpace(limit.Model) == "*" {
			wildcard = &limit
			continue
		}
		if strings.EqualFold(strings.TrimSpace(limit.Model), model) {
			return limit, true
		}
	}
	if wildcard != nil {
		return *wildcard, true
	}
	return models.ModelRateLimitConfig{}, false
}

func minuteTTL(now time.Time) time.Duration {
	nextMinute := now.Truncate(time.Minute).Add(time.Minute)
	return nextMinute.Sub(now) + 5*time.Second
}

func keyRequestLimitCounter(groupID, keyID uint, limit *models.KeyRequestLimitConfig, now time.Time) (string, time.Duration, error) {
	return requestLimitCounter(fmt.Sprintf("usage:group:%d:key:%d:req", groupID, keyID), limit, now)
}

func modelRequestLimitCounter(groupID, keyID uint, modelKey string, limit *models.RequestLimitConfig, now time.Time) (string, time.Duration, error) {
	return requestLimitCounter(fmt.Sprintf("usage:group:%d:key:%d:model:%s:req", groupID, keyID, modelKey), limit, now)
}

func requestLimitCounter(prefix string, limit *models.RequestLimitConfig, now time.Time) (string, time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(limit.ResetMode)) {
	case "", "interval":
		interval := time.Duration(limit.IntervalMinutes) * time.Minute
		if interval <= 0 {
			interval = 24 * time.Hour
		}
		bucket := now.Unix() / int64(interval.Seconds())
		windowEnd := time.Unix((bucket+1)*int64(interval.Seconds()), 0)
		return fmt.Sprintf("%s:interval:%d", prefix, bucket), windowEnd.Sub(now) + 5*time.Second, nil
	case "daily":
		windowStart, windowEnd, err := dailyResetWindow(now, limit.ResetTime)
		if err != nil {
			return "", 0, err
		}
		return fmt.Sprintf("%s:daily:%s", prefix, windowStart.In(pacificLocation).Format("20060102T150405")), windowEnd.Sub(now) + 5*time.Second, nil
	default:
		return "", 0, fmt.Errorf("unsupported request reset mode: %s", limit.ResetMode)
	}
}

func dailyResetWindow(now time.Time, resetTime string) (time.Time, time.Time, error) {
	if strings.TrimSpace(resetTime) == "" {
		resetTime = "00:00"
	}

	layout := "15:04"
	if strings.Count(resetTime, ":") == 2 {
		layout = "15:04:05"
	}
	parsed, err := time.ParseInLocation(layout, resetTime, pacificLocation)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid daily reset_time %q: %w", resetTime, err)
	}

	pacificNow := now.In(pacificLocation)
	todayReset := time.Date(pacificNow.Year(), pacificNow.Month(), pacificNow.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, pacificLocation)
	if pacificNow.Before(todayReset) {
		return todayReset.AddDate(0, 0, -1), todayReset, nil
	}
	return todayReset, todayReset.AddDate(0, 0, 1), nil
}

func loadPacificLocation() *time.Location {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return time.FixedZone("America/Los_Angeles", -8*60*60)
	}
	return location
}

func modelCounterToken(model string) string {
	model = strings.TrimSpace(strings.ToLower(model))
	if model == "" {
		model = "_unknown"
	}
	sum := sha1.Sum([]byte(model))
	return hex.EncodeToString(sum[:])
}
