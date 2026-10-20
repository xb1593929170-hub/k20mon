package collector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yourname/k20mon/server/model"
)

const (
	batteryPath = "sys/class/power_supply/qcom-battery"
	chargerPath = "sys/class/power_supply/pm8150b-charger"
)

// Collector reads Linux procfs/sysfs without requiring root privileges.
// root is injectable so the parsing logic can be tested on any workstation.
type Collector struct {
	root string

	mu            sync.Mutex
	lastSampleAt  time.Time
	lastRXBytes   int64
	lastTXBytes   int64
	lastCPUIdle   int64
	lastCPUTotal  int64
	haveNetSample bool
	haveCPUSample bool

	thermalReadDisabled bool
}

func New() *Collector {
	return NewWithRoot(string(filepath.Separator))
}

func NewWithRoot(root string) *Collector {
	if root == "" {
		root = string(filepath.Separator)
	}
	return &Collector{root: filepath.Clean(root)}
}

func (c *Collector) Collect(ctx context.Context) (model.Metrics, error) {
	if err := ctx.Err(); err != nil {
		return model.Metrics{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().UTC()
	warnings := make([]string, 0, 4)
	battery := c.collectBattery(&warnings)
	charger := c.collectCharger(&battery, &warnings)
	battery.ConsumptionW = estimateConsumptionW(battery.Status, battery.PowerW, charger.InputPowerW)
	network := c.collectNetwork(&warnings)
	system := c.collectSystem(&warnings)

	if err := ctx.Err(); err != nil {
		return model.Metrics{}, err
	}
	return model.Metrics{
		Timestamp: now,
		Battery:   battery,
		Charger:   charger,
		Network:   network,
		System:    system,
		Warnings:  warnings,
	}, nil
}

// CollectPower reads only the battery and charger paths used by the dashboard's
// live power status. It intentionally skips procfs, thermal, CPU-frequency, and
// GPU reads so a slow or unavailable system node cannot block the current API.
func (c *Collector) CollectPower(ctx context.Context) (model.Metrics, error) {
	if err := ctx.Err(); err != nil {
		return model.Metrics{}, err
	}

	now := time.Now().UTC()
	warnings := make([]string, 0, 2)
	battery := c.collectBattery(&warnings)
	charger := c.collectCharger(&battery, &warnings)
	battery.ConsumptionW = estimateConsumptionW(battery.Status, battery.PowerW, charger.InputPowerW)
	return model.Metrics{
		Timestamp: now,
		Battery:   battery,
		Charger:   charger,
		Warnings:  warnings,
	}, nil
}

func (c *Collector) path(parts ...string) string {
	all := make([]string, 0, len(parts)+1)
	all = append(all, c.root)
	all = append(all, parts...)
	return filepath.Join(all...)
}

func (c *Collector) read(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(raw)), true
}

func (c *Collector) readInt(path string) (*int64, bool) {
	raw, ok := c.read(path)
	if !ok || raw == "" {
		return nil, false
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		if decimal, decimalErr := strconv.ParseFloat(raw, 64); decimalErr == nil {
			value = int64(decimal)
		} else {
			return nil, false
		}
	}
	return &value, true
}

func (c *Collector) readUevent(dir string) map[string]string {
	raw, ok := c.read(filepath.Join(dir, "uevent"))
	if !ok {
		return map[string]string{}
	}
	values := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		key, value, found := strings.Cut(line, "=")
		if found && key != "" {
			values[key] = value
		}
	}
	return values
}

func (c *Collector) readIntOrUevent(path string, values map[string]string, key string) *int64 {
	primary, primaryOK := c.readInt(path)
	fallback, fallbackOK := intFromUevent(values, key)
	return chooseInt(primary, primaryOK, fallback, fallbackOK)
}

func (c *Collector) readBatteryInt(dir string, values map[string]string, name, key string) *int64 {
	return c.readIntOrUevent(filepath.Join(dir, name), values, key)
}

func (c *Collector) collectBattery(warnings *[]string) model.BatteryMetrics {
	dir := c.path(batteryPath)
	uevent := c.readUevent(dir)
	result := model.BatteryMetrics{Status: c.readString(filepath.Join(dir, "status"), "Unknown")}

	result.CapacityPercent = c.readBatteryInt(dir, uevent, "capacity", "POWER_SUPPLY_CAPACITY")
	voltageRaw := c.readBatteryInt(dir, uevent, "voltage_now", "POWER_SUPPLY_VOLTAGE_NOW")
	currentRaw := c.readBatteryInt(dir, uevent, "current_now", "POWER_SUPPLY_CURRENT_NOW")
	tempRaw := c.readBatteryInt(dir, uevent, "temp", "POWER_SUPPLY_TEMP")
	result.ChargeNowUah = c.readBatteryInt(dir, uevent, "charge_now", "POWER_SUPPLY_CHARGE_NOW")
	result.ChargeFullUah = c.readBatteryInt(dir, uevent, "charge_full", "POWER_SUPPLY_CHARGE_FULL")
	result.ChargeFullDesignUah = c.readBatteryInt(dir, uevent, "charge_full_design", "POWER_SUPPLY_CHARGE_FULL_DESIGN")
	result.EnergyNowUwh = c.readBatteryInt(dir, uevent, "energy_now", "POWER_SUPPLY_ENERGY_NOW")
	result.EnergyFullUwh = c.readBatteryInt(dir, uevent, "energy_full", "POWER_SUPPLY_ENERGY_FULL")
	result.EnergyFullDesignUwh = c.readBatteryInt(dir, uevent, "energy_full_design", "POWER_SUPPLY_ENERGY_FULL_DESIGN")
	result.CycleCount = c.readBatteryInt(dir, uevent, "cycle_count", "POWER_SUPPLY_CYCLE_COUNT")

	if result.ChargeFullUah != nil && result.ChargeFullDesignUah != nil && *result.ChargeFullDesignUah > 0 {
		health := float64(*result.ChargeFullUah) / float64(*result.ChargeFullDesignUah) * 100
		result.HealthPercent = floatPtr(health)
	} else if result.EnergyFullUwh != nil && result.EnergyFullDesignUwh != nil && *result.EnergyFullDesignUwh > 0 {
		health := float64(*result.EnergyFullUwh) / float64(*result.EnergyFullDesignUwh) * 100
		result.HealthPercent = floatPtr(health)
	}

	if voltageRaw != nil {
		result.VoltageV = floatPtr(float64(*voltageRaw) / 1_000_000)
	}
	if currentRaw != nil {
		currentMA := float64(*currentRaw) / 1_000
		result.CurrentMA = &currentMA
		if voltageRaw != nil {
			power := float64(*voltageRaw) * absFloat64(float64(*currentRaw)) / 1_000_000_000_000
			result.PowerW = &power
		}
	}
	if tempRaw != nil {
		temperature := float64(*tempRaw) / 10
		result.TemperatureC = &temperature
	}

	chargeForETA := result.ChargeFullUah
	if chargeForETA == nil {
		chargeForETA = result.ChargeFullDesignUah
	}
	if result.CapacityPercent != nil && result.CurrentMA != nil && chargeForETA != nil {
		eta := estimateETA(result.Status, *result.CapacityPercent, *result.CurrentMA, *chargeForETA)
		result.ETAMinutes = eta
		result.Trickle = result.Status == "Charging" && absFloat64(*result.CurrentMA) < 50
	}
	if result.Status == "" || result.Status == "Unknown" {
		*warnings = append(*warnings, "battery status is unavailable")
	}
	return result
}

func (c *Collector) collectCharger(battery *model.BatteryMetrics, warnings *[]string) model.ChargerMetrics {
	dir := c.path(chargerPath)
	uevent := c.readUevent(dir)
	result := model.ChargerMetrics{
		Status:        c.readString(filepath.Join(dir, "status"), "Unknown"),
		Health:        c.readString(filepath.Join(dir, "health"), "Unknown"),
		BatteryStatus: battery.Status,
		USBTypeRaw:    c.readString(filepath.Join(dir, "usb_type"), "Unknown"),
	}
	result.USBType = selectedUSBType(result.USBTypeRaw)

	if online := c.readIntOrUevent(filepath.Join(dir, "online"), uevent, "POWER_SUPPLY_ONLINE"); online != nil {
		value := *online != 0
		result.Online = &value
	}
	voltageRaw := c.readIntOrUevent(filepath.Join(dir, "voltage_now"), uevent, "POWER_SUPPLY_VOLTAGE_NOW")
	currentRaw := c.readIntOrUevent(filepath.Join(dir, "current_now"), uevent, "POWER_SUPPLY_CURRENT_NOW")
	result.CurrentMaxUA = c.readIntOrUevent(filepath.Join(dir, "current_max"), uevent, "POWER_SUPPLY_CURRENT_MAX")
	if voltageRaw != nil && *voltageRaw > 0 && *voltageRaw < 20_000_000 {
		result.VoltageV = floatPtr(float64(*voltageRaw) / 1_000_000)
	}
	if currentRaw != nil {
		currentMA := float64(*currentRaw) / 1_000
		result.CurrentMA = &currentMA
		if result.VoltageV != nil {
			power := *result.VoltageV * currentMA / 1_000
			result.InputPowerW = &power
		}
	}

	if typeC := c.firstTypeC(); typeC != nil {
		result.TypeC = typeC
		result.PDNegotiated = typeC.PDNegotiated
	}
	if result.CurrentMaxUA != nil && *result.CurrentMaxUA <= 1_000_000 {
		result.Throttled = true
		result.ThrottledReason = fmt.Sprintf("input current limit is %dmA", *result.CurrentMaxUA/1000)
	}
	if result.Status == "Unknown" && result.Online == nil {
		*warnings = append(*warnings, "charger status is unavailable")
	}
	return result
}

func estimateConsumptionW(status string, batteryPowerW, inputPowerW *float64) *float64 {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "charging":
		if batteryPowerW == nil || inputPowerW == nil {
			return nil
		}
		consumption := *inputPowerW - *batteryPowerW
		if consumption < 0 {
			consumption = 0
		}
		return floatPtr(consumption)
	case "discharging", "not charging", "full":
		if batteryPowerW == nil {
			return nil
		}
		return floatPtr(*batteryPowerW)
	default:
		return nil
	}
}

func (c *Collector) firstTypeC() *model.TypeCMetrics {
	matches, _ := filepath.Glob(c.path("sys/class/power_supply/*typec*"))
	for _, dir := range matches {
		onlineRaw, _ := c.readInt(filepath.Join(dir, "online"))
		online := onlineRaw != nil && *onlineRaw != 0
		usbType := c.readString(filepath.Join(dir, "usb_type"), "")
		active := selectedUSBType(usbType)
		result := &model.TypeCMetrics{
			Name:          filepath.Base(dir),
			Online:        online,
			USBType:       usbType,
			USBTypeActive: active,
			PDNegotiated:  online && strings.HasPrefix(active, "PD"),
		}
		if raw, ok := c.readInt(filepath.Join(dir, "voltage_now")); ok {
			result.VoltageV = floatPtr(float64(*raw) / 1_000_000)
		}
		if raw, ok := c.readInt(filepath.Join(dir, "current_now")); ok {
			result.CurrentMA = floatPtr(float64(*raw) / 1_000)
		}
		return result
	}
	return nil
}

func (c *Collector) collectNetwork(warnings *[]string) model.NetworkMetrics {
	interfaces := c.interfaces()
	iface := c.defaultInterface()
	result := model.NetworkMetrics{
		DefaultInterface: iface,
		Interfaces:       interfaces,
	}
	for _, item := range interfaces {
		if item.Name == iface {
			result.RXBytes = item.RXBytes
			result.TXBytes = item.TXBytes
			break
		}
	}
	now := time.Now()
	if c.haveNetSample {
		seconds := now.Sub(c.lastSampleAt).Seconds()
		if seconds > 0 {
			result.RXBPS = floatPtr(float64(result.RXBytes-c.lastRXBytes) / seconds)
			result.TXBPS = floatPtr(float64(result.TXBytes-c.lastTXBytes) / seconds)
		}
	}
	c.lastSampleAt = now
	c.lastRXBytes = result.RXBytes
	c.lastTXBytes = result.TXBytes
	c.haveNetSample = true
	if iface == "" {
		*warnings = append(*warnings, "default network interface is unavailable")
	}
	return result
}

func (c *Collector) interfaces() []model.InterfaceMetrics {
	matches, _ := filepath.Glob(c.path("sys/class/net/*"))
	result := make([]model.InterfaceMetrics, 0, len(matches))
	for _, dir := range matches {
		name := filepath.Base(dir)
		rxValue, rxOK := c.readInt(filepath.Join(dir, "statistics/rx_bytes"))
		txValue, txOK := c.readInt(filepath.Join(dir, "statistics/tx_bytes"))
		speedValue, speedOK := c.readInt(filepath.Join(dir, "speed"))
		result = append(result, model.InterfaceMetrics{
			Name:      name,
			State:     c.readString(filepath.Join(dir, "operstate"), "unknown"),
			RXBytes:   intValue(rxValue, rxOK),
			TXBytes:   intValue(txValue, txOK),
			SpeedMbps: speedValueIfAvailable(speedValue, speedOK),
		})
	}
	return result
}

func (c *Collector) defaultInterface() string {
	if raw, ok := c.read(c.path("proc/net/route")); ok {
		for _, line := range strings.Split(raw, "\n")[1:] {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == "00000000" {
				return fields[0]
			}
		}
	}
	if c.root != string(filepath.Separator) {
		return ""
	}
	command := exec.Command("ip", "route", "show", "default")
	output, err := command.Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(output))
	for index, field := range fields {
		if field == "dev" && index+1 < len(fields) {
			return fields[index+1]
		}
	}
	return ""
}

func (c *Collector) collectSystem(warnings *[]string) model.SystemMetrics {
	result := model.SystemMetrics{
		Temperatures: make(map[string]float64),
		Governors:    make([]string, 0),
	}
	result.CPUCores = intPtr(int64(runtime.NumCPU()))

	if raw, ok := c.read(c.path("proc/stat")); ok {
		result.CPUUsagePercent = c.cpuUsage(raw)
	}
	if raw, ok := c.read(c.path("proc/loadavg")); ok {
		fields := strings.Fields(raw)
		if len(fields) >= 3 {
			result.Load1 = parseFloatPtr(fields[0])
			result.Load5 = parseFloatPtr(fields[1])
			result.Load15 = parseFloatPtr(fields[2])
		}
	}
	result.FrequencyMHz, result.Governors = c.cpuFrequencies()
	result.Temperatures, result.HottestZone, result.HottestC = c.temperatures()
	result.Memory = c.memory()
	result.GPU = c.gpu()
	if raw, ok := c.read(c.path("proc/uptime")); ok {
		if fields := strings.Fields(raw); len(fields) > 0 {
			if seconds, err := strconv.ParseFloat(fields[0], 64); err == nil {
				result.UptimeSeconds = intPtr(int64(seconds))
			}
		}
	}
	if result.CPUUsagePercent == nil && result.Load1 == nil {
		*warnings = append(*warnings, "procfs CPU metrics are unavailable")
	}
	return result
}

func (c *Collector) gpu() model.GPUMetrics {
	const devfreqRoot = "sys/devices/platform/soc@0/2c00000.gpu/devfreq/2c00000.gpu"
	const genericDevfreqRoot = "sys/class/devfreq"
	result := model.GPUMetrics{}

	root := c.path(devfreqRoot)
	if _, ok := c.read(filepath.Join(root, "cur_freq")); !ok {
		matches, _ := filepath.Glob(c.path(genericDevfreqRoot, "*gpu*"))
		if len(matches) == 0 {
			matches, _ = filepath.Glob(c.path(genericDevfreqRoot, "*"))
		}
		for _, candidate := range matches {
			if _, ok := c.read(filepath.Join(candidate, "cur_freq")); ok {
				root = candidate
				break
			}
		}
	}

	current := c.readFreqMHz(filepath.Join(root, "cur_freq"))
	minimum := c.readFreqMHz(filepath.Join(root, "min_freq"))
	maximum := c.readFreqMHz(filepath.Join(root, "max_freq"))
	if current == nil && maximum == nil && minimum == nil {
		return result
	}

	result.Available = true
	result.Device = c.readString(filepath.Join(root, "name"), filepath.Base(root))
	result.Governor = c.readString(filepath.Join(root, "governor"), "")
	result.CurrentFrequencyMHz = current
	result.MinimumFrequencyMHz = minimum
	result.MaximumFrequencyMHz = maximum

	uevent := c.readUevent(c.path("sys/devices/platform/soc@0/2c00000.gpu"))
	result.Driver = uevent["DRIVER"]
	if result.Driver == "" {
		result.Driver = c.readString(filepath.Join(root, "driver"), "")
	}
	result.Model = c.readString(c.path("sys/devices/platform/soc@0/2c00000.gpu/of_node/compatible"), "")
	if result.Model == "" {
		result.Model = c.readString(c.path("sys/devices/platform/soc@0/2c00000.gpu/uevent"), "")
		if strings.Contains(result.Model, "qcom,adreno-") {
			for _, line := range strings.Split(result.Model, "\n") {
				if strings.HasPrefix(line, "OF_COMPATIBLE_0=") {
					result.Model = strings.TrimPrefix(line, "OF_COMPATIBLE_0=")
					break
				}
			}
		}
	}
	result.UtilizationPercent = c.readPercent(
		filepath.Join(root, "load"),
		filepath.Join(root, "gpu_busy_percentage"),
		filepath.Join(root, "gpu_busy"),
	)
	if value := c.readTemperature(c.path("sys/class/thermal")); value != nil {
		result.TemperatureC = value
	}
	return result
}

func (c *Collector) readFreqMHz(path string) *float64 {
	value, ok := c.readInt(path)
	if !ok {
		return nil
	}
	frequency := float64(*value) / 1_000_000
	return &frequency
}

func (c *Collector) readPercent(paths ...string) *float64 {
	for _, path := range paths {
		value, ok := c.readInt(path)
		if !ok {
			continue
		}
		percent := clamp(float64(*value), 0, 100)
		return &percent
	}
	return nil
}

func (c *Collector) readTemperature(root string) *float64 {
	matches, _ := filepath.Glob(filepath.Join(root, "thermal_zone*"))
	for _, dir := range matches {
		name := strings.ToLower(c.readString(filepath.Join(dir, "type"), ""))
		if !strings.Contains(name, "gpu") && !strings.Contains(name, "kgsl") {
			continue
		}
		if value, ok := c.readInt(filepath.Join(dir, "temp")); ok {
			temperature := float64(*value) / 1000
			return &temperature
		}
	}
	return nil
}

func (c *Collector) cpuUsage(raw string) *float64 {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var values []int64
		for _, field := range fields[1:] {
			value, err := strconv.ParseInt(field, 10, 64)
			if err != nil {
				return nil
			}
			values = append(values, value)
		}
		total := int64(0)
		for _, value := range values {
			total += value
		}
		idle := values[3]
		if len(values) > 4 {
			idle += values[4]
		}
		if c.haveCPUSample {
			totalDelta := total - c.lastCPUTotal
			idleDelta := idle - c.lastCPUIdle
			if totalDelta > 0 {
				usage := (1 - float64(idleDelta)/float64(totalDelta)) * 100
				c.lastCPUIdle, c.lastCPUTotal = idle, total
				return floatPtr(clamp(usage, 0, 100))
			}
		}
		c.lastCPUIdle, c.lastCPUTotal = idle, total
		c.haveCPUSample = true
		return floatPtr(0)
	}
	return nil
}

func (c *Collector) cpuFrequencies() (model.FrequencyMetrics, []string) {
	matches, _ := filepath.Glob(c.path("sys/devices/system/cpu/cpu[0-9]*/cpufreq"))
	values := make([]float64, 0, len(matches))
	governorSet := map[string]struct{}{}
	for _, dir := range matches {
		if raw, ok := c.readInt(filepath.Join(dir, "scaling_cur_freq")); ok {
			values = append(values, float64(*raw)/1000)
		}
		if governor := c.readString(filepath.Join(dir, "scaling_governor"), ""); governor != "" {
			governorSet[governor] = struct{}{}
		}
	}
	governors := make([]string, 0, len(governorSet))
	for governor := range governorSet {
		governors = append(governors, governor)
	}
	return model.FrequencyMetrics{
		AverageMHz: averagePtr(values),
		MinimumMHz: minPtr(values),
		MaximumMHz: maxPtr(values),
	}, governors
}

func (c *Collector) temperatures() (map[string]float64, string, *float64) {
	values := make(map[string]float64)
	if c.thermalReadDisabled {
		return values, "", nil
	}

	matches, _ := filepath.Glob(c.path("sys/class/thermal/thermal_zone*"))
	const thermalReadTimeout = 200 * time.Millisecond
	const thermalReadBudget = 750 * time.Millisecond
	started := time.Now()
	for _, dir := range matches {
		if time.Since(started) >= thermalReadBudget {
			c.thermalReadDisabled = true
			break
		}
		name, ok := c.readWithTimeout(filepath.Join(dir, "type"), thermalReadTimeout)
		if !ok {
			c.thermalReadDisabled = true
			break
		}
		raw, ok := c.readWithTimeout(filepath.Join(dir, "temp"), thermalReadTimeout)
		if !ok {
			c.thermalReadDisabled = true
			break
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			continue
		}
		if strings.TrimSpace(name) == "" {
			name = filepath.Base(dir)
		}
		values[name] = float64(value) / 1000
	}
	var hottestZone string
	var hottest *float64
	for name, value := range values {
		if hottest == nil || value > *hottest {
			hottestZone = name
			hottest = floatPtr(value)
		}
	}
	return values, hottestZone, hottest
}

func (c *Collector) readWithTimeout(path string, timeout time.Duration) (string, bool) {
	result := make(chan string, 1)
	go func() {
		raw, err := os.ReadFile(path)
		if err != nil {
			result <- ""
			return
		}
		result <- strings.TrimSpace(string(raw))
	}()

	select {
	case raw := <-result:
		return raw, raw != ""
	case <-time.After(timeout):
		return "", false
	}
}

func (c *Collector) memory() model.MemoryMetrics {
	raw, ok := c.read(c.path("proc/meminfo"))
	if !ok {
		return model.MemoryMetrics{}
	}
	values := map[string]int64{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if value, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				values[strings.TrimSuffix(fields[0], ":")] = value
			}
		}
	}
	total := values["MemTotal"] / 1024
	available := values["MemAvailable"] / 1024
	used := total - available
	swapTotal := values["SwapTotal"] / 1024
	swapFree := values["SwapFree"] / 1024
	swapUsed := swapTotal - swapFree
	var usedPercent, swapPercent float64
	if total > 0 {
		usedPercent = float64(used) / float64(total) * 100
	}
	if swapTotal > 0 {
		swapPercent = float64(swapUsed) / float64(swapTotal) * 100
	}
	return model.MemoryMetrics{
		TotalMB:     intPtr(total),
		UsedMB:      intPtr(used),
		AvailableMB: intPtr(available),
		UsedPercent: floatPtr(usedPercent),
		SwapTotalMB: intPtr(swapTotal),
		SwapUsedMB:  intPtr(swapUsed),
		SwapUsedPct: floatPtr(swapPercent),
	}
}

func (c *Collector) readString(path, fallback string) string {
	if value, ok := c.read(path); ok {
		return value
	}
	return fallback
}

func chooseInt(primary *int64, primaryOK bool, fallback *int64, fallbackOK bool) *int64 {
	if primaryOK && primary != nil {
		return primary
	}
	if fallbackOK && fallback != nil {
		return fallback
	}
	return nil
}

func intFromUevent(values map[string]string, key string) (*int64, bool) {
	raw, ok := values[key]
	if !ok {
		return nil, false
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	return &value, err == nil
}

func intValue(value *int64, ok bool) int64 {
	if !ok || value == nil {
		return 0
	}
	return *value
}

func speedValueIfAvailable(value *int64, ok bool) *int64 {
	if !ok {
		return nil
	}
	return value
}

func estimateETA(status string, capacity int64, currentMA float64, designUah int64) *int64 {
	if designUah <= 0 {
		return nil
	}
	designAh := float64(designUah) / 1_000_000
	var remainingAh float64
	switch status {
	case "Charging":
		if currentMA <= 50 {
			return nil
		}
		remainingAh = float64(100-capacity) / 100 * designAh
	case "Discharging":
		currentMA = absFloat64(currentMA)
		if currentMA <= 50 {
			return nil
		}
		remainingAh = float64(capacity) / 100 * designAh
	default:
		return nil
	}
	minutes := remainingAh / (currentMA / 1000) * 60
	value := int64(minutes)
	return &value
}

func selectedUSBType(raw string) string {
	raw = strings.TrimSpace(raw)
	if start := strings.Index(raw, "["); start >= 0 {
		if end := strings.Index(raw[start+1:], "]"); end >= 0 {
			return raw[start+1 : start+1+end]
		}
	}
	return raw
}

func floatPtr(value float64) *float64 { return &value }
func intPtr(value int64) *int64       { return &value }
func parseFloatPtr(raw string) *float64 {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &value
}

func averagePtr(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return floatPtr(total / float64(len(values)))
}

func minPtr(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	minimum := values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
	}
	return floatPtr(minimum)
}

func maxPtr(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	maximum := values[0]
	for _, value := range values[1:] {
		if value > maximum {
			maximum = value
		}
	}
	return floatPtr(maximum)
}

func absFloat64(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
