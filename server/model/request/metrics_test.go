package request

import (
	"testing"
	"time"
)

func TestRangeDefaultsToADashboardSizedWindow(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	from, to, limit, err := (MetricsHistoryRequest{}).Range(now)
	if err != nil {
		t.Fatalf("Range returned an error: %v", err)
	}
	if !to.Equal(now) || !from.Equal(now.Add(-24*time.Hour)) {
		t.Fatalf("unexpected default range: %s - %s", from, to)
	}
	if limit != 20_000 {
		t.Fatalf("expected 20,000 default rows, got %d", limit)
	}
}

func TestRangeCapsExplicitHistoryLimit(t *testing.T) {
	_, _, limit, err := (MetricsHistoryRequest{Limit: 999_999}).Range(time.Now())
	if err != nil {
		t.Fatalf("Range returned an error: %v", err)
	}
	if limit != 600_000 {
		t.Fatalf("expected 600,000 row cap, got %d", limit)
	}
}
