package service

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/yourname/k20mon/server/model"
)

const (
	batteryHistoryMaxRows = 600_000
	batterySummaryTTL     = time.Minute
	batteryMaxSampleGap   = 30 * time.Second
)

func (s *MetricsService) batteryHistorySummary(ctx context.Context, now time.Time) (model.BatteryHistorySummary, error) {
	if err := ctx.Err(); err != nil {
		return model.BatteryHistorySummary{}, err
	}

	s.batteryHistoryMu.Lock()
	cached := s.batteryHistoryValue
	if !s.batteryHistoryAt.IsZero() && time.Since(s.batteryHistoryAt) < batterySummaryTTL {
		s.batteryHistoryMu.Unlock()
		return cached, nil
	}
	if !s.batteryHistoryRefreshing {
		s.batteryHistoryRefreshing = true
		go s.refreshBatteryHistory()
	}
	s.batteryHistoryMu.Unlock()

	// History calculation can scan many days of samples on a small device.
	// Return the last value immediately and let the next request observe the
	// refreshed result instead of blocking the dashboard request.
	return cached, nil
}

func (s *MetricsService) refreshBatteryHistory() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := s.loadBatteryHistory(ctx, time.Now().UTC())
	s.batteryHistoryMu.Lock()
	defer s.batteryHistoryMu.Unlock()
	s.batteryHistoryRefreshing = false
	if err != nil {
		return
	}
	s.batteryHistoryValue = result
	s.batteryHistoryAt = time.Now()
}

func (s *MetricsService) loadBatteryHistory(ctx context.Context, now time.Time) (model.BatteryHistorySummary, error) {
	if err := ctx.Err(); err != nil {
		return model.BatteryHistorySummary{}, err
	}
	conn := conn()
	if conn == nil {
		return model.BatteryHistorySummary{}, ErrStoreUnavailable
	}

	columns := []string{
		"sampled_at",
		"battery_capacity",
		"battery_current_ma",
		"battery_voltage_v",
		"battery_status",
		"battery_design_capacity_uah",
		"battery_power_w",
		"battery_consumption_w",
	}
	if conn.Dialector.Name() == "mysql" {
		// Older MySQL snapshots only populated the JSON payload.
		columns[1] = "COALESCE(battery_capacity, CAST(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(payload, '$.battery.capacityPercent')), 'null') AS SIGNED)) AS battery_capacity"
		columns[2] = "COALESCE(battery_current_ma, CAST(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(payload, '$.battery.currentMa')), 'null') AS DECIMAL(16,3))) AS battery_current_ma"
		columns[3] = "COALESCE(battery_voltage_v, CAST(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(payload, '$.battery.voltageV')), 'null') AS DECIMAL(16,6))) AS battery_voltage_v"
		columns[4] = "COALESCE(NULLIF(battery_status, ''), NULLIF(JSON_UNQUOTE(JSON_EXTRACT(payload, '$.battery.status')), 'null')) AS battery_status"
		columns[5] = "COALESCE(battery_design_capacity_uah, CAST(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(payload, '$.battery.chargeFullDesignUah')), 'null') AS UNSIGNED)) AS battery_design_capacity_uah"
		columns[6] = "COALESCE(battery_power_w, CAST(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(payload, '$.battery.powerW')), 'null') AS DECIMAL(16,6))) AS battery_power_w"
		columns[7] = "COALESCE(battery_consumption_w, CAST(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(payload, '$.battery.consumptionW')), 'null') AS DECIMAL(16,6))) AS battery_consumption_w"
	}
	var rows []model.MetricSample
	err := conn.WithContext(ctx).
		Select(strings.Join(columns, ", ")).
		Where("sampled_at >= ? AND sampled_at <= ?", now.Add(-retentionDuration()), now).
		Order("sampled_at DESC").
		Limit(batteryHistoryMaxRows).
		Find(&rows).Error
	if err != nil {
		return model.BatteryHistorySummary{}, err
	}
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}

	result := deriveBatteryHistory(rows)
	return result, nil
}

func deriveBatteryHistory(rows []model.MetricSample) model.BatteryHistorySummary {
	result := model.BatteryHistorySummary{
		SampleCount: len(rows),
	}
	if len(rows) == 0 {
		return result
	}
	if len(rows) > 1 {
		span := rows[len(rows)-1].SampledAt.Sub(rows[0].SampledAt)
		if span > 0 {
			result.WindowHours = int(math.Ceil(span.Hours()))
		}
	}

	var designUah int64
	var chargedUah, dischargedUah float64
	var dischargeEnergyWh, deviceEnergyWh, deviceEnergyHours float64
	var dischargeHours float64
	var capacityEstimates []float64
	var run capacityRun

	finishRun := func() {
		span := absInt64(run.endPercent - run.startPercent)
		if run.hasCurrent && run.completeCurrent && span >= 5 && run.chargeUah > 0 {
			estimate := run.chargeUah * 100 / float64(span)
			if estimate >= 100_000 && estimate <= 20_000_000 {
				capacityEstimates = append(capacityEstimates, estimate)
			}
		}
		run = capacityRun{}
	}

	for index := range rows {
		row := rows[index]
		if row.BatteryDesignCapacityUah != nil && *row.BatteryDesignCapacityUah > 0 {
			designUah = *row.BatteryDesignCapacityUah
		}
		if index == 0 {
			continue
		}

		previous := rows[index-1]
		elapsed := row.SampledAt.Sub(previous.SampledAt)
		if elapsed <= 0 || elapsed > batteryMaxSampleGap {
			finishRun()
			continue
		}
		hours := elapsed.Hours()
		status := strings.ToLower(strings.TrimSpace(row.BatteryStatus))
		percentDelta := int64(0)
		hasPercentDelta := row.BatteryCapacity != nil && previous.BatteryCapacity != nil
		if hasPercentDelta {
			percentDelta = *row.BatteryCapacity - *previous.BatteryCapacity
		}

		direction := ""
		switch status {
		case "charging":
			direction = "charge"
		case "discharging", "not charging":
			direction = "discharge"
		}

		var amountUah float64
		if direction != "" {
			if previous.BatteryCurrentMA != nil && row.BatteryCurrentMA != nil {
				amountUah = (math.Abs(*previous.BatteryCurrentMA) + math.Abs(*row.BatteryCurrentMA)) / 2 * hours * 1000
			} else if designUah > 0 && hasPercentDelta &&
				((direction == "charge" && percentDelta > 0) || (direction == "discharge" && percentDelta < 0)) {
				amountUah = float64(absInt64(percentDelta)) * float64(designUah) / 100
				result.UsedDesignFallback = true
			}
		}

		voltage := averagePointer(previous.BatteryVoltageV, row.BatteryVoltageV)
		if direction == "charge" {
			chargedUah += amountUah
		} else if direction == "discharge" {
			dischargedUah += amountUah
			dischargeHours += hours
			if voltage != nil {
				dischargeEnergyWh += amountUah * *voltage / 1_000_000
			}
		}

		if power := averageConsumptionPower(previous, row, status); power != nil {
			deviceEnergyWh += *power * hours
			deviceEnergyHours += hours
		}

		validCapacityDirection := direction == "discharge" && hasPercentDelta && percentDelta <= 0 ||
			direction == "charge" && hasPercentDelta && percentDelta >= 0
		if !validCapacityDirection || (run.direction != "" && run.direction != direction) {
			finishRun()
			continue
		}
		if run.direction == "" {
			run.direction = direction
			run.startPercent = *previous.BatteryCapacity
			run.completeCurrent = true
		}
		run.endPercent = *row.BatteryCapacity
		if previous.BatteryCurrentMA != nil && row.BatteryCurrentMA != nil {
			run.hasCurrent = true
			run.chargeUah += (math.Abs(*previous.BatteryCurrentMA) + math.Abs(*row.BatteryCurrentMA)) / 2 * hours * 1000
		} else {
			run.completeCurrent = false
		}
	}
	finishRun()

	if chargedUah > 0 {
		value := int64(math.Round(chargedUah))
		result.ChargeAddedUah = &value
	}
	if dischargedUah > 0 {
		value := int64(math.Round(dischargedUah))
		result.DischargeUsedUah = &value
	}
	if dischargeEnergyWh > 0 {
		result.DischargeEnergyWh = floatPointer(dischargeEnergyWh)
	}
	if deviceEnergyHours > 0 && deviceEnergyWh > 0 {
		result.AverageConsumptionW = floatPointer(deviceEnergyWh / deviceEnergyHours)
	} else if dischargeHours > 0 && dischargeEnergyWh > 0 {
		result.AverageConsumptionW = floatPointer(dischargeEnergyWh / dischargeHours)
	}

	if len(capacityEstimates) > 0 {
		sort.Float64s(capacityEstimates)
		median := capacityEstimates[len(capacityEstimates)/2]
		if len(capacityEstimates)%2 == 0 {
			median = (capacityEstimates[len(capacityEstimates)/2-1] + median) / 2
		}
		estimate := int64(math.Round(median))
		result.EstimatedFullUah = &estimate
		result.CapacityEstimateSamples = len(capacityEstimates)
		if designUah > 0 {
			result.EstimatedHealthPercent = floatPointer(median / float64(designUah) * 100)
		}
	}

	fullUah := designUah
	if result.EstimatedFullUah != nil {
		fullUah = *result.EstimatedFullUah
	}
	if fullUah > 0 {
		if len(rows) > 0 && rows[len(rows)-1].BatteryCapacity != nil {
			remaining := int64(math.Round(float64(fullUah) * float64(*rows[len(rows)-1].BatteryCapacity) / 100))
			result.EstimatedRemainingUah = &remaining
		}
		if dischargedUah > 0 {
			result.EquivalentCycles = floatPointer(dischargedUah / float64(fullUah))
		}
	}

	return result
}

type capacityRun struct {
	startPercent    int64
	endPercent      int64
	chargeUah       float64
	hasCurrent      bool
	completeCurrent bool
	direction       string
}

func averageConsumptionPower(previous, current model.MetricSample, currentStatus string) *float64 {
	if previous.BatteryConsumptionW != nil && current.BatteryConsumptionW != nil {
		return averagePointer(previous.BatteryConsumptionW, current.BatteryConsumptionW)
	}
	if currentStatus == "discharging" || currentStatus == "not charging" {
		return averagePointer(previous.BatteryPowerW, current.BatteryPowerW)
	}
	return nil
}

func averagePointer(left, right *float64) *float64 {
	if left == nil || right == nil {
		return nil
	}
	return floatPointer((*left + *right) / 2)
}

func floatPointer(value float64) *float64 {
	return &value
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
