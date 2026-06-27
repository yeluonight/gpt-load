package services

import "testing"

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
