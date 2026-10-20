package service

import (
	"testing"
	"time"

	"github.com/yourname/k20mon/server/model"
)

func TestDeriveBatteryHistoryEstimatesCapacityFromDischarge(t *testing.T) {
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	rows := make([]model.MetricSample, 21)
	for index := range rows {
		capacity := int64(100 - index/2)
		current := 1000.0
		voltage := 4.0
		power := 4.0
		rows[index] = model.MetricSample{
			SampledAt:                start.Add(time.Duration(index) * 30 * time.Second),
			BatteryCapacity:          &capacity,
			BatteryCurrentMA:         &current,
			BatteryVoltageV:          &voltage,
			BatteryStatus:            "Discharging",
			BatteryDesignCapacityUah: int64Pointer(2_000_000),
			BatteryPowerW:            &power,
		}
	}

	summary := deriveBatteryHistory(rows)
	if summary.WindowHours != 1 {
		t.Fatalf("expected one hour of available samples, got %d", summary.WindowHours)
	}
	if summary.EstimatedFullUah == nil || *summary.EstimatedFullUah < 1_660_000 || *summary.EstimatedFullUah > 1_680_000 {
		t.Fatalf("unexpected estimated full capacity: %#v", summary.EstimatedFullUah)
	}
	if summary.DischargeUsedUah == nil || *summary.DischargeUsedUah < 166_000 || *summary.DischargeUsedUah > 168_000 {
		t.Fatalf("unexpected discharge amount: %#v", summary.DischargeUsedUah)
	}
	if summary.DischargeEnergyWh == nil || *summary.DischargeEnergyWh < 0.66 || *summary.DischargeEnergyWh > 0.68 {
		t.Fatalf("unexpected discharge energy: %#v", summary.DischargeEnergyWh)
	}
	if summary.AverageConsumptionW == nil || *summary.AverageConsumptionW != 4 {
		t.Fatalf("unexpected average consumption: %#v", summary.AverageConsumptionW)
	}
}

func TestDeriveBatteryHistoryUsesDesignCapacityForSOCTotals(t *testing.T) {
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	before, after := int64(42), int64(40)
	rows := []model.MetricSample{
		{
			SampledAt:                start,
			BatteryCapacity:          &before,
			BatteryStatus:            "Discharging",
			BatteryDesignCapacityUah: int64Pointer(4_000_000),
		},
		{
			SampledAt:                start.Add(5 * time.Second),
			BatteryCapacity:          &after,
			BatteryStatus:            "Discharging",
			BatteryDesignCapacityUah: int64Pointer(4_000_000),
		},
	}

	summary := deriveBatteryHistory(rows)
	if summary.DischargeUsedUah == nil || *summary.DischargeUsedUah != 80_000 {
		t.Fatalf("unexpected estimated discharge amount: %#v", summary.DischargeUsedUah)
	}
	if !summary.UsedDesignFallback {
		t.Fatal("expected SOC/design-capacity fallback to be reported")
	}
	if summary.EstimatedFullUah != nil {
		t.Fatalf("must not estimate full capacity without measured current: %#v", summary.EstimatedFullUah)
	}
}

func TestDeriveBatteryHistoryEstimatesCapacityFromCharge(t *testing.T) {
	start := time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)
	rows := make([]model.MetricSample, 21)
	for index := range rows {
		capacity := int64(50 + index/2)
		current := 1000.0
		rows[index] = model.MetricSample{
			SampledAt:        start.Add(time.Duration(index) * 30 * time.Second),
			BatteryCapacity:  &capacity,
			BatteryCurrentMA: &current,
			BatteryStatus:    "Charging",
		}
	}

	summary := deriveBatteryHistory(rows)
	if summary.EstimatedFullUah == nil || *summary.EstimatedFullUah < 1_660_000 || *summary.EstimatedFullUah > 1_680_000 {
		t.Fatalf("unexpected estimated full capacity: %#v", summary.EstimatedFullUah)
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}
