package collector

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectReadsK20PowerAndSystemMetrics(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/status", "Charging\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/capacity", "72\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/voltage_now", "4000000\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/current_now", "1500000\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/temp", "315\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/charge_now", "2880000\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/charge_full", "3900000\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/charge_full_design", "4000000\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/cycle_count", "42\n")
	writeFixture(t, root, "sys/class/power_supply/pm8150b-charger/status", "Charging\n")
	writeFixture(t, root, "sys/class/power_supply/pm8150b-charger/online", "1\n")
	writeFixture(t, root, "sys/class/power_supply/pm8150b-charger/usb_type", " [PD]\n")
	writeFixture(t, root, "sys/class/power_supply/pm8150b-charger/voltage_now", "5000000\n")
	writeFixture(t, root, "sys/class/power_supply/pm8150b-charger/current_now", "2000000\n")
	writeFixture(t, root, "sys/class/power_supply/pm8150b-charger/current_max", "3000000\n")
	writeFixture(t, root, "sys/class/power_supply/usb-typec/online", "1\n")
	writeFixture(t, root, "sys/class/power_supply/usb-typec/usb_type", "[PD]\n")
	writeFixture(t, root, "proc/loadavg", "1.00 0.50 0.25 1/100 123\n")
	writeFixture(t, root, "proc/stat", "cpu  100 0 100 800 0 0 0 0 0 0\n")
	writeFixture(t, root, "proc/meminfo", "MemTotal:       2048000 kB\nMemAvailable:   1024000 kB\nSwapTotal:      0 kB\nSwapFree:       0 kB\n")
	writeFixture(t, root, "proc/uptime", "123.45 0.00\n")
	writeFixture(t, root, "proc/net/route", "Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\nwlan0 00000000 01020304 0003 0 0 0 00000000 0 0 0\n")
	writeFixture(t, root, "sys/class/net/wlan0/operstate", "up\n")
	writeFixture(t, root, "sys/class/net/wlan0/statistics/rx_bytes", "1000\n")
	writeFixture(t, root, "sys/class/net/wlan0/statistics/tx_bytes", "2000\n")
	writeFixture(t, root, "sys/devices/platform/soc@0/2c00000.gpu/devfreq/2c00000.gpu/cur_freq", "257000000\n")
	writeFixture(t, root, "sys/devices/platform/soc@0/2c00000.gpu/devfreq/2c00000.gpu/min_freq", "192000000\n")
	writeFixture(t, root, "sys/devices/platform/soc@0/2c00000.gpu/devfreq/2c00000.gpu/max_freq", "585000000\n")
	writeFixture(t, root, "sys/devices/platform/soc@0/2c00000.gpu/devfreq/2c00000.gpu/name", "2c00000.gpu\n")
	writeFixture(t, root, "sys/devices/platform/soc@0/2c00000.gpu/devfreq/2c00000.gpu/governor", "simple_ondemand\n")
	writeFixture(t, root, "sys/devices/platform/soc@0/2c00000.gpu/uevent", "DRIVER=adreno\n")

	snapshot, err := NewWithRoot(root).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if snapshot.Battery.CapacityPercent == nil || *snapshot.Battery.CapacityPercent != 72 {
		t.Fatalf("unexpected capacity: %#v", snapshot.Battery.CapacityPercent)
	}
	if snapshot.Battery.VoltageV == nil || *snapshot.Battery.VoltageV != 4 {
		t.Fatalf("unexpected voltage: %#v", snapshot.Battery.VoltageV)
	}
	if snapshot.Battery.PowerW == nil || *snapshot.Battery.PowerW != 6 ||
		snapshot.Battery.ConsumptionW == nil || *snapshot.Battery.ConsumptionW != 4 {
		t.Fatalf("unexpected power metrics: %#v", snapshot.Battery)
	}
	if snapshot.Battery.ChargeNowUah == nil || *snapshot.Battery.ChargeNowUah != 2880000 ||
		snapshot.Battery.ChargeFullUah == nil || *snapshot.Battery.ChargeFullUah != 3900000 ||
		snapshot.Battery.HealthPercent == nil || *snapshot.Battery.HealthPercent != 97.5 ||
		snapshot.Battery.CycleCount == nil || *snapshot.Battery.CycleCount != 42 {
		t.Fatalf("unexpected battery capacity metrics: %#v", snapshot.Battery)
	}
	if snapshot.Charger.USBType != "PD" || !snapshot.Charger.PDNegotiated {
		t.Fatalf("unexpected charger: %#v", snapshot.Charger)
	}
	if snapshot.Network.DefaultInterface != "wlan0" {
		t.Fatalf("unexpected interface: %#v", snapshot.Network)
	}
	if snapshot.System.Load1 == nil || *snapshot.System.Load1 != 1 {
		t.Fatalf("unexpected load: %#v", snapshot.System.Load1)
	}
	if !snapshot.System.GPU.Available ||
		snapshot.System.GPU.CurrentFrequencyMHz == nil ||
		*snapshot.System.GPU.CurrentFrequencyMHz != 257 ||
		snapshot.System.GPU.MaximumFrequencyMHz == nil ||
		*snapshot.System.GPU.MaximumFrequencyMHz != 585 ||
		snapshot.System.GPU.Driver != "adreno" ||
		snapshot.System.GPU.Governor != "simple_ondemand" {
		t.Fatalf("unexpected gpu metrics: %#v", snapshot.System.GPU)
	}
}

func TestCollectReadsEnergyCapacityFallback(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/status", "Discharging\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/capacity", "55\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/voltage_now", "3800000\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/current_now", "-1000000\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/energy_now", "8000000\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/energy_full", "14000000\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/energy_full_design", "16000000\n")

	snapshot, err := NewWithRoot(root).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if snapshot.Battery.EnergyFullUwh == nil || *snapshot.Battery.EnergyFullUwh != 14000000 ||
		snapshot.Battery.HealthPercent == nil || *snapshot.Battery.HealthPercent != 87.5 {
		t.Fatalf("unexpected energy battery metrics: %#v", snapshot.Battery)
	}
	if snapshot.Battery.ConsumptionW == nil || *snapshot.Battery.ConsumptionW != 3.8 {
		t.Fatalf("unexpected discharging power: %#v", snapshot.Battery.ConsumptionW)
	}
}

func TestCollectReportsMissingKernelNodesAsWarnings(t *testing.T) {
	snapshot, err := NewWithRoot(t.TempDir()).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if snapshot.Battery.CapacityPercent != nil || snapshot.Charger.TypeC != nil {
		t.Fatalf("expected unavailable power metrics, got %#v %#v", snapshot.Battery, snapshot.Charger.TypeC)
	}
	if !strings.Contains(strings.Join(snapshot.Warnings, "\n"), "battery status") {
		t.Fatalf("expected battery warning, got %#v", snapshot.Warnings)
	}
}

func TestCollectHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewWithRoot(t.TempDir()).Collect(ctx); err == nil {
		t.Fatal("expected canceled context error")
	}
}

func TestCollectPowerSkipsSystemAndNetwork(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/status", "Charging\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/capacity", "50\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/voltage_now", "4000000\n")
	writeFixture(t, root, "sys/class/power_supply/qcom-battery/current_now", "1000000\n")
	writeFixture(t, root, "sys/class/power_supply/pm8150b-charger/voltage_now", "5000000\n")
	writeFixture(t, root, "sys/class/power_supply/pm8150b-charger/current_now", "2000000\n")

	snapshot, err := NewWithRoot(root).CollectPower(context.Background())
	if err != nil {
		t.Fatalf("collect power: %v", err)
	}
	if snapshot.Battery.CapacityPercent == nil || *snapshot.Battery.CapacityPercent != 50 {
		t.Fatalf("unexpected capacity: %#v", snapshot.Battery.CapacityPercent)
	}
	if snapshot.System.CPUCores != nil || snapshot.Network.DefaultInterface != "" {
		t.Fatalf("CollectPower must not read system/network: %#v %#v", snapshot.System, snapshot.Network)
	}
}

func TestEstimateConsumptionW(t *testing.T) {
	power := func(value float64) *float64 { return &value }
	if got := estimateConsumptionW("Charging", power(6), power(10)); got == nil || *got != 4 {
		t.Fatalf("charging: %#v", got)
	}
	if got := estimateConsumptionW("Charging", power(6), nil); got != nil {
		t.Fatalf("charging without input power should be nil: %#v", got)
	}
	if got := estimateConsumptionW("Discharging", power(3.8), nil); got == nil || *got != 3.8 {
		t.Fatalf("discharging: %#v", got)
	}
	if got := estimateConsumptionW("Unknown", power(3.8), power(10)); got != nil {
		t.Fatalf("unknown status should be nil: %#v", got)
	}
}

func TestEstimateETA(t *testing.T) {
	if got := estimateETA("Charging", 50, 1000, 4_000_000); got == nil || *got <= 0 {
		t.Fatalf("charging eta: %#v", got)
	}
	if got := estimateETA("Charging", 50, 10, 4_000_000); got != nil {
		t.Fatalf("trickle current should skip eta: %#v", got)
	}
	if got := estimateETA("Full", 100, 1000, 4_000_000); got != nil {
		t.Fatalf("full status should skip eta: %#v", got)
	}
	if got := estimateETA("Discharging", 50, -1000, 4_000_000); got == nil || *got <= 0 {
		t.Fatalf("discharging eta: %#v", got)
	}
	if got := estimateETA("Charging", 50, 1000, 0); got != nil {
		t.Fatalf("zero design capacity should skip eta: %#v", got)
	}
}

func TestSelectedUSBType(t *testing.T) {
	if got := selectedUSBType(" [PD]"); got != "PD" {
		t.Fatalf("bracket form: %q", got)
	}
	if got := selectedUSBType("SDP"); got != "SDP" {
		t.Fatalf("plain form: %q", got)
	}
	if got := selectedUSBType(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
}

func writeFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
