package services

import (
	"gpt-load/internal/models"
	"testing"
)

func TestNormalizeDailyResetTimeAcceptsSingleDigitHour(t *testing.T) {
	got, err := normalizeDailyResetTime("0:00")
	if err != nil {
		t.Fatalf("normalizeDailyResetTime returned error: %v", err)
	}
	if got != "00:00" {
		t.Fatalf("normalizeDailyResetTime = %q, want %q", got, "00:00")
	}

	got, err = normalizeDailyResetTime("9:05:07")
	if err != nil {
		t.Fatalf("normalizeDailyResetTime returned error: %v", err)
	}
	if got != "09:05:07" {
		t.Fatalf("normalizeDailyResetTime = %q, want %q", got, "09:05:07")
	}
}

func TestNormalizeDailyResetTimeRejectsBadMinutes(t *testing.T) {
	if _, err := normalizeDailyResetTime("0:0"); err == nil {
		t.Fatal("normalizeDailyResetTime accepted single-digit minute")
	}
	if _, err := normalizeDailyResetTime("24:00"); err == nil {
		t.Fatal("normalizeDailyResetTime accepted hour 24")
	}
}

func TestValidateGroupRuntimeConfigAcceptsModelRequestLimitOnly(t *testing.T) {
	config := models.GroupConfig{
		ModelRateLimits: []models.ModelRateLimitConfig{
			{
				Model: "gpt-test",
				RequestLimit: &models.RequestLimitConfig{
					MaxRequests:     5,
					ResetMode:       "interval",
					IntervalMinutes: 60,
				},
			},
		},
	}

	if err := validateGroupRuntimeConfig(config); err != nil {
		t.Fatalf("validateGroupRuntimeConfig returned error: %v", err)
	}
}

func TestNormalizeGroupRuntimeConfigNormalizesModelRequestLimit(t *testing.T) {
	config := models.GroupConfig{
		ModelRateLimits: []models.ModelRateLimitConfig{
			{
				Model: " gpt-test ",
				RequestLimit: &models.RequestLimitConfig{
					MaxRequests: 5,
					ResetTime:   "0:00",
				},
			},
		},
	}

	normalizeGroupRuntimeConfig(&config)
	limit := config.ModelRateLimits[0]
	if limit.Model != "gpt-test" {
		t.Fatalf("model = %q, want gpt-test", limit.Model)
	}
	if limit.RequestLimit.ResetMode != "daily" {
		t.Fatalf("reset mode = %q, want daily", limit.RequestLimit.ResetMode)
	}
	if limit.RequestLimit.ResetTime != "00:00" {
		t.Fatalf("reset time = %q, want 00:00", limit.RequestLimit.ResetTime)
	}
}
