package model

import "time"

// Metrics is the read-only device snapshot exposed by the K20Mon dashboard.
// Pointer fields deliberately preserve the difference between zero and a
// metric that is not available on the running kernel.
type Metrics struct {
	Timestamp time.Time      `json:"timestamp"`
	Battery   BatteryMetrics `json:"battery"`
	Charger   ChargerMetrics `json:"charger"`
	Network   NetworkMetrics `json:"network"`
	System    SystemMetrics  `json:"system"`
	Warnings  []string       `json:"warnings"`
}

// MetricSample stores the complete snapshot as JSON and keeps common chart
// fields denormalized for efficient retention and summary queries.
type MetricSample struct {
	ID                       uint      `gorm:"primaryKey" json:"id"`
	SampledAt                time.Time `gorm:"index;not null" json:"sampledAt"`
	Payload                  string    `gorm:"type:longtext;not null" json:"-"`
	BatteryCapacity          *int64    `json:"batteryCapacity"`
	BatteryCurrentMA         *float64  `gorm:"column:battery_current_ma" json:"batteryCurrentMa"`
	BatteryVoltageV          *float64  `gorm:"column:battery_voltage_v" json:"batteryVoltageV"`
	BatteryStatus            string    `gorm:"column:battery_status;size:24" json:"batteryStatus"`
	BatteryDesignCapacityUah *int64    `gorm:"column:battery_design_capacity_uah" json:"batteryDesignCapacityUah"`
	BatteryPowerW            *float64  `json:"batteryPowerW"`
	BatteryConsumptionW      *float64  `json:"batteryConsumptionW"`
	BatteryTemperatureC      *float64  `json:"batteryTemperatureC"`
	CPUUsagePercent          *float64  `json:"cpuUsagePercent"`
	HottestTemperatureC      *float64  `json:"hottestTemperatureC"`
	MemoryUsedPercent        *float64  `json:"memoryUsedPercent"`
	NetworkRXBPS             *float64  `json:"networkRxBps"`
	NetworkTXBPS             *float64  `json:"networkTxBps"`
}

func (MetricSample) TableName() string {
	return "k20mon_metric_samples"
}

type BatteryMetrics struct {
	CapacityPercent     *int64   `json:"capacityPercent"`
	Status              string   `json:"status"`
	VoltageV            *float64 `json:"voltageV"`
	CurrentMA           *float64 `json:"currentMa"`
	PowerW              *float64 `json:"powerW"`
	TemperatureC        *float64 `json:"temperatureC"`
	ChargeNowUah        *int64   `json:"chargeNowUah"`
	ChargeFullUah       *int64   `json:"chargeFullUah"`
	ChargeFullDesignUah *int64   `json:"chargeFullDesignUah"`
	EnergyNowUwh        *int64   `json:"energyNowUwh"`
	EnergyFullUwh       *int64   `json:"energyFullUwh"`
	EnergyFullDesignUwh *int64   `json:"energyFullDesignUwh"`
	ConsumptionW        *float64 `json:"consumptionW"`
	HealthPercent       *float64 `json:"healthPercent"`
	CycleCount          *int64   `json:"cycleCount"`
	ETAMinutes          *int64   `json:"etaMinutes"`
	Trickle             bool     `json:"trickle"`
}

type ChargerMetrics struct {
	Status          string        `json:"status"`
	Online          *bool         `json:"online"`
	Health          string        `json:"health"`
	BatteryStatus   string        `json:"batteryStatus"`
	USBType         string        `json:"usbType"`
	USBTypeRaw      string        `json:"usbTypeRaw"`
	VoltageV        *float64      `json:"voltageV"`
	CurrentMA       *float64      `json:"currentMa"`
	InputPowerW     *float64      `json:"inputPowerW"`
	CurrentMaxUA    *int64        `json:"currentMaxUa"`
	PDNegotiated    bool          `json:"pdNegotiated"`
	Throttled       bool          `json:"throttled"`
	ThrottledReason string        `json:"throttledReason"`
	TypeC           *TypeCMetrics `json:"typeC"`
}

type TypeCMetrics struct {
	Name          string   `json:"name"`
	Online        bool     `json:"online"`
	VoltageV      *float64 `json:"voltageV"`
	CurrentMA     *float64 `json:"currentMa"`
	USBType       string   `json:"usbType"`
	USBTypeActive string   `json:"usbTypeActive"`
	PDNegotiated  bool     `json:"pdNegotiated"`
}

type NetworkMetrics struct {
	DefaultInterface string             `json:"defaultInterface"`
	RXBytes          int64              `json:"rxBytes"`
	TXBytes          int64              `json:"txBytes"`
	RXBPS            *float64           `json:"rxBps"`
	TXBPS            *float64           `json:"txBps"`
	Interfaces       []InterfaceMetrics `json:"interfaces"`
}

type InterfaceMetrics struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	RXBytes   int64  `json:"rxBytes"`
	TXBytes   int64  `json:"txBytes"`
	SpeedMbps *int64 `json:"speedMbps"`
}

type SystemMetrics struct {
	CPUUsagePercent *float64           `json:"cpuUsagePercent"`
	Load1           *float64           `json:"load1"`
	Load5           *float64           `json:"load5"`
	Load15          *float64           `json:"load15"`
	CPUCores        *int64             `json:"cpuCores"`
	FrequencyMHz    FrequencyMetrics   `json:"frequencyMhz"`
	Governors       []string           `json:"governors"`
	Temperatures    map[string]float64 `json:"temperatures"`
	HottestZone     string             `json:"hottestZone"`
	HottestC        *float64           `json:"hottestC"`
	Memory          MemoryMetrics      `json:"memory"`
	GPU             GPUMetrics         `json:"gpu"`
	UptimeSeconds   *int64             `json:"uptimeSeconds"`
}

type GPUMetrics struct {
	Available           bool     `json:"available"`
	Model               string   `json:"model"`
	Driver              string   `json:"driver"`
	Device              string   `json:"device"`
	Governor            string   `json:"governor"`
	CurrentFrequencyMHz *float64 `json:"currentFrequencyMhz"`
	MinimumFrequencyMHz *float64 `json:"minimumFrequencyMhz"`
	MaximumFrequencyMHz *float64 `json:"maximumFrequencyMhz"`
	UtilizationPercent  *float64 `json:"utilizationPercent"`
	TemperatureC        *float64 `json:"temperatureC"`
}

type FrequencyMetrics struct {
	AverageMHz *float64 `json:"averageMhz"`
	MinimumMHz *float64 `json:"minimumMhz"`
	MaximumMHz *float64 `json:"maximumMhz"`
}

type MemoryMetrics struct {
	TotalMB     *int64   `json:"totalMb"`
	UsedMB      *int64   `json:"usedMb"`
	AvailableMB *int64   `json:"availableMb"`
	UsedPercent *float64 `json:"usedPercent"`
	SwapTotalMB *int64   `json:"swapTotalMb"`
	SwapUsedMB  *int64   `json:"swapUsedMb"`
	SwapUsedPct *float64 `json:"swapUsedPercent"`
}

type NumericSummary struct {
	Count int      `json:"count"`
	Min   *float64 `json:"min"`
	Max   *float64 `json:"max"`
	Avg   *float64 `json:"avg"`
}

type MetricsSummary struct {
	From                time.Time             `json:"from"`
	To                  time.Time             `json:"to"`
	SampleCount         int                   `json:"sampleCount"`
	BatteryHistory      BatteryHistorySummary `json:"batteryHistory"`
	BatteryCapacity     NumericSummary        `json:"batteryCapacity"`
	BatteryPowerW       NumericSummary        `json:"batteryPowerW"`
	BatteryConsumptionW NumericSummary        `json:"batteryConsumptionW"`
	BatteryTemperatureC NumericSummary        `json:"batteryTemperatureC"`
	CPUUsagePercent     NumericSummary        `json:"cpuUsagePercent"`
	HottestTemperatureC NumericSummary        `json:"hottestTemperatureC"`
	MemoryUsedPercent   NumericSummary        `json:"memoryUsedPercent"`
	NetworkRXBPS        NumericSummary        `json:"networkRxBps"`
	NetworkTXBPS        NumericSummary        `json:"networkTxBps"`
}

type BatteryHistorySummary struct {
	WindowHours             int      `json:"windowHours"`
	SampleCount             int      `json:"sampleCount"`
	ChargeAddedUah          *int64   `json:"chargeAddedUah"`
	DischargeUsedUah        *int64   `json:"dischargeUsedUah"`
	DischargeEnergyWh       *float64 `json:"dischargeEnergyWh"`
	AverageConsumptionW     *float64 `json:"averageConsumptionW"`
	EstimatedFullUah        *int64   `json:"estimatedFullUah"`
	EstimatedHealthPercent  *float64 `json:"estimatedHealthPercent"`
	EstimatedRemainingUah   *int64   `json:"estimatedRemainingUah"`
	EquivalentCycles        *float64 `json:"equivalentCycles"`
	CapacityEstimateSamples int      `json:"capacityEstimateSamples"`
	UsedDesignFallback      bool     `json:"usedDesignFallback"`
}
