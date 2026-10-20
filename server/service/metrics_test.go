package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yourname/k20mon/server/collector"
	"github.com/yourname/k20mon/server/model"
)

func TestCurrentUsesFreshPublishedSnapshot(t *testing.T) {
	service := NewMetricsServiceWithCollector(collector.NewWithRoot(t.TempDir()))
	expected := model.Metrics{
		Timestamp: time.Now().UTC(),
		Battery: model.BatteryMetrics{
			Status: "Charging",
		},
	}
	service.publish(expected)

	actual, err := service.Current(context.Background())
	if err != nil {
		t.Fatalf("Current returned an error: %v", err)
	}
	if actual.Timestamp != expected.Timestamp {
		t.Fatalf("Current collected instead of using the fresh snapshot: got %v want %v", actual.Timestamp, expected.Timestamp)
	}
	if actual.Battery.Status != "Charging" {
		t.Fatalf("unexpected battery status: %q", actual.Battery.Status)
	}
}

func TestCurrentRefreshesPowerWhenFullSnapshotIsStale(t *testing.T) {
	root := t.TempDir()
	writeCollectorFixture(t, root, "sys/class/power_supply/qcom-battery/status", "Charging\n")
	writeCollectorFixture(t, root, "sys/class/power_supply/qcom-battery/current_now", "600000\n")
	writeCollectorFixture(t, root, "sys/class/power_supply/qcom-battery/voltage_now", "4000000\n")
	writeCollectorFixture(t, root, "sys/class/power_supply/qcom-battery/capacity", "42\n")
	writeCollectorFixture(t, root, "sys/class/power_supply/pm8150b-charger/status", "Charging\n")
	writeCollectorFixture(t, root, "sys/class/power_supply/pm8150b-charger/online", "1\n")
	writeCollectorFixture(t, root, "sys/class/power_supply/pm8150b-charger/voltage_now", "5000000\n")
	writeCollectorFixture(t, root, "sys/class/power_supply/pm8150b-charger/current_now", "700000\n")
	writeCollectorFixture(t, root, "sys/class/power_supply/pm8150b-charger/current_max", "3000000\n")

	service := NewMetricsServiceWithCollector(collector.NewWithRoot(root))
	service.publish(model.Metrics{
		Timestamp: time.Now().UTC().Add(-currentSnapshotTTL - time.Second),
		System:    model.SystemMetrics{Load1: floatPtr(1.5)},
	})

	actual, err := service.Current(context.Background())
	if err != nil {
		t.Fatalf("Current returned an error: %v", err)
	}
	if actual.Battery.Status != "Charging" {
		t.Fatalf("expected live battery status, got %q", actual.Battery.Status)
	}
	if actual.Charger.Status != "Charging" {
		t.Fatalf("expected live charger status, got %q", actual.Charger.Status)
	}
	if actual.System.Load1 == nil || *actual.System.Load1 != 1.5 {
		t.Fatalf("expected stale system snapshot to be reused, got %#v", actual.System.Load1)
	}
}

func writeCollectorFixture(t *testing.T, root, relative, value string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
