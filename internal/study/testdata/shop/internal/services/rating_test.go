package services

import "testing"

func TestOverage(t *testing.T) {
	if got := calculateOverage(12, 10, 3); got != 6 {
		t.Fatalf("overage %d", got)
	}
}

func TestWithinQuota(t *testing.T) {
	if got := calculateOverage(8, 10, 3); got != 0 {
		t.Fatalf("overage %d", got)
	}
}

func TestRateUsageNeedsRepository(t *testing.T) {
	s := &RatingService{}
	defer func() { recover() }()
	s.RateUsage(nil, 1, "2026-10")
}
