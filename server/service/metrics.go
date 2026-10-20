package service

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/yourname/k20mon/server/collector"
	"github.com/yourname/k20mon/server/model"
	metricsRequest "github.com/yourname/k20mon/server/model/request"
)

var Metrics = NewMetricsService()

const currentSnapshotTTL = 8 * time.Second

type MetricsService struct {
	collector *collector.Collector
	store     *sampleStore

	mu          sync.RWMutex
	latest      model.Metrics
	hasLatest   bool
	subscribers map[chan model.Metrics]struct{}

	samplerMu      sync.Mutex
	samplerStarted bool

	batteryHistoryMu         sync.Mutex
	batteryHistoryAt         time.Time
	batteryHistoryValue      model.BatteryHistorySummary
	batteryHistoryRefreshing bool
}

func NewMetricsService() *MetricsService {
	return &MetricsService{
		collector:   collector.New(),
		store:       &sampleStore{},
		subscribers: make(map[chan model.Metrics]struct{}),
	}
}

func NewMetricsServiceWithCollector(value *collector.Collector) *MetricsService {
	return &MetricsService{
		collector:   value,
		store:       &sampleStore{},
		subscribers: make(map[chan model.Metrics]struct{}),
	}
}

func (s *MetricsService) Current(ctx context.Context) (model.Metrics, error) {
	if snapshot, ok := s.Latest(); ok {
		age := time.Since(snapshot.Timestamp)
		if age >= 0 && age <= currentSnapshotTTL {
			return snapshot, nil
		}
	}

	power, err := s.collector.CollectPower(ctx)
	if err != nil {
		return model.Metrics{}, err
	}

	// Keep the live battery/charger values independent from slow system probes.
	// Reuse the last known system/network sections when a full sample is stale.
	if previous, ok := s.Latest(); ok {
		power.System = previous.System
		power.Network = previous.Network
	}
	snapshot := power
	s.publish(snapshot)
	return snapshot, nil
}

func (s *MetricsService) sample(ctx context.Context) (model.Metrics, error) {
	snapshot, err := s.collector.Collect(ctx)
	if err != nil {
		return model.Metrics{}, err
	}
	// Publish first so /metrics/current stays responsive even if MySQL is slow.
	s.publish(snapshot)
	if err := s.store.Save(ctx, snapshot); err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "history persistence unavailable")
		s.publish(snapshot)
	}
	return snapshot, nil
}

func (s *MetricsService) publish(snapshot model.Metrics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = snapshot
	s.hasLatest = true
	for subscriber := range s.subscribers {
		select {
		case subscriber <- snapshot:
		default:
		}
	}
}

func (s *MetricsService) Latest() (model.Metrics, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest, s.hasLatest
}

func (s *MetricsService) Subscribe(ctx context.Context) (<-chan model.Metrics, func()) {
	channel := make(chan model.Metrics, 1)
	s.mu.Lock()
	s.subscribers[channel] = struct{}{}
	s.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subscribers, channel)
			close(channel)
			s.mu.Unlock()
		})
	}
	go func() {
		<-ctx.Done()
		unsubscribe()
	}()
	return channel, unsubscribe
}

func (s *MetricsService) StartSampler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	s.samplerMu.Lock()
	if s.samplerStarted {
		s.samplerMu.Unlock()
		return
	}
	s.samplerStarted = true
	s.samplerMu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		lastPrune := time.Time{}
		for {
			_, _ = s.sample(ctx)
			if lastPrune.IsZero() || time.Since(lastPrune) >= time.Hour {
				retention := retentionDuration()
				_ = s.store.Prune(ctx, time.Now().UTC().Add(-retention))
				lastPrune = time.Now()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *MetricsService) History(ctx context.Context, query metricsRequest.MetricsHistoryRequest) ([]model.Metrics, error) {
	from, to, limit, err := query.Range(time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if !storeAvailable() {
		return nil, ErrStoreUnavailable
	}
	conn := conn()
	var rows []model.MetricSample
	err = conn.WithContext(ctx).
		Where("sampled_at >= ? AND sampled_at <= ?", from, to).
		Order("sampled_at DESC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]model.Metrics, 0, len(rows))
	for index := len(rows) - 1; index >= 0; index-- {
		row := rows[index]
		var snapshot model.Metrics
		if err := json.Unmarshal([]byte(row.Payload), &snapshot); err != nil {
			continue
		}
		result = append(result, snapshot)
	}
	return result, nil
}

func (s *MetricsService) Summary(ctx context.Context, query metricsRequest.MetricsHistoryRequest) (model.MetricsSummary, error) {
	from, to, _, err := query.Range(time.Now().UTC())
	if err != nil {
		return model.MetricsSummary{}, err
	}
	history, err := s.History(ctx, query)
	if err != nil {
		return model.MetricsSummary{}, err
	}
	summary := model.MetricsSummary{From: from, To: to, SampleCount: len(history)}
	batteryHistory, err := s.batteryHistorySummary(ctx, time.Now().UTC())
	if err != nil {
		return model.MetricsSummary{}, err
	}
	summary.BatteryHistory = batteryHistory
	summary.BatteryCapacity = summarizeInt(history, func(item model.Metrics) *int64 {
		return item.Battery.CapacityPercent
	})
	summary.BatteryPowerW = summarizeFloat(history, func(item model.Metrics) *float64 {
		return item.Battery.PowerW
	})
	summary.BatteryConsumptionW = summarizeFloat(history, func(item model.Metrics) *float64 {
		return item.Battery.ConsumptionW
	})
	summary.BatteryTemperatureC = summarizeFloat(history, func(item model.Metrics) *float64 {
		return item.Battery.TemperatureC
	})
	summary.CPUUsagePercent = summarizeFloat(history, func(item model.Metrics) *float64 {
		return item.System.CPUUsagePercent
	})
	summary.HottestTemperatureC = summarizeFloat(history, func(item model.Metrics) *float64 {
		return item.System.HottestC
	})
	summary.MemoryUsedPercent = summarizeFloat(history, func(item model.Metrics) *float64 {
		return item.System.Memory.UsedPercent
	})
	summary.NetworkRXBPS = summarizeFloat(history, func(item model.Metrics) *float64 {
		return item.Network.RXBPS
	})
	summary.NetworkTXBPS = summarizeFloat(history, func(item model.Metrics) *float64 {
		return item.Network.TXBPS
	})
	return summary, nil
}

type sampleStore struct{}

func (s *sampleStore) Save(ctx context.Context, snapshot model.Metrics) error {
	conn := conn()
	if conn == nil {
		return ErrStoreUnavailable
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	row := model.MetricSample{
		SampledAt:                snapshot.Timestamp,
		Payload:                  string(payload),
		BatteryCapacity:          snapshot.Battery.CapacityPercent,
		BatteryCurrentMA:         snapshot.Battery.CurrentMA,
		BatteryVoltageV:          snapshot.Battery.VoltageV,
		BatteryStatus:            snapshot.Battery.Status,
		BatteryDesignCapacityUah: snapshot.Battery.ChargeFullDesignUah,
		BatteryPowerW:            snapshot.Battery.PowerW,
		BatteryConsumptionW:      snapshot.Battery.ConsumptionW,
		BatteryTemperatureC:      snapshot.Battery.TemperatureC,
		CPUUsagePercent:          snapshot.System.CPUUsagePercent,
		HottestTemperatureC:      snapshot.System.HottestC,
		MemoryUsedPercent:        snapshot.System.Memory.UsedPercent,
		NetworkRXBPS:             snapshot.Network.RXBPS,
		NetworkTXBPS:             snapshot.Network.TXBPS,
	}
	return conn.WithContext(ctx).Create(&row).Error
}

func (s *sampleStore) Prune(ctx context.Context, before time.Time) error {
	conn := conn()
	if conn == nil {
		return ErrStoreUnavailable
	}
	return conn.WithContext(ctx).
		Where("sampled_at < ?", before).
		Delete(&model.MetricSample{}).Error
}

func summarizeInt(items []model.Metrics, read func(model.Metrics) *int64) model.NumericSummary {
	values := make([]float64, 0, len(items))
	for _, item := range items {
		if value := read(item); value != nil {
			values = append(values, float64(*value))
		}
	}
	return summarize(values)
}

func summarizeFloat(items []model.Metrics, read func(model.Metrics) *float64) model.NumericSummary {
	values := make([]float64, 0, len(items))
	for _, item := range items {
		if value := read(item); value != nil {
			values = append(values, *value)
		}
	}
	return summarize(values)
}

func summarize(values []float64) model.NumericSummary {
	if len(values) == 0 {
		return model.NumericSummary{}
	}
	minimum, maximum, total := values[0], values[0], 0.0
	for _, value := range values {
		if value < minimum {
			minimum = value
		}
		if value > maximum {
			maximum = value
		}
		total += value
	}
	return model.NumericSummary{
		Count: len(values),
		Min:   floatPtr(minimum),
		Max:   floatPtr(maximum),
		Avg:   floatPtr(total / float64(len(values))),
	}
}

func retentionDuration() time.Duration {
	hours := 720.0
	if raw := os.Getenv("K20M_RETENTION_H"); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil && parsed > 0 {
			hours = parsed
		}
	}
	return time.Duration(hours * float64(time.Hour))
}

func floatPtr(value float64) *float64 {
	return &value
}
