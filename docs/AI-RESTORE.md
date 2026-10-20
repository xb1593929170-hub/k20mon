# K20Mon 完整还原指南（可直接交给 AI）

> **这份文档的用途**：把整段内容粘贴给任意一个能写代码的 AI，它就能从零把 K20Mon 这个项目完整重建出来，包括目录结构、每个文件的职责、接口契约、关键算法、构建方式和验收标准。
>
> **使用方法**：整段复制 → 粘贴给 AI → 追加一句 `按这份文档把项目完整实现出来，先输出文件树，再逐个文件给完整代码，不要省略、不要用 TODO 占位`。

---

## 1. 项目是什么

K20Mon 是一个**跑在 Android 手机上的 Linux 环境里的设备监控 + 运维控制台**。

目标设备：Redmi K20 Pro（高通 SM8150 / `raphael`），Android 上跑 Ubuntu 26.04 chroot，手机长期插电当小型服务器用。

它要解决的具体问题：

1. 手机当服务器时电池长期满电会鼓包，需要一个能远程设「限充阈值 + 电流限制」的面板；
2. 需要看实时功耗、温度、CPU/GPU 频率、内存、网络速率，判断是不是被限频或过热；
3. 需要估算电池实际健康度（设计容量 vs 实际满充容量），判断电池衰减；
4. 需要远程管 Wi-Fi、frp 内网穿透隧道、Cloudflare Tunnel；
5. 需要一个「服务宫格」把手机和同网段其他机器上的自建服务汇总成一个可点开的入口页。

**关键约束**：后端跑在容器里、**没有 root 权限**，但上面这些写操作（改 sysfs、跑 nmcli、重启 frp 容器）**必须 root**。所以整个架构的核心是：**后端只做只读采集 + HTTP，所有特权操作交给一个独立运行的 root 代理，通过 Unix socket 通信，动作走白名单。**

---

## 2. 技术栈与硬性约束

| 层 | 选型 | 说明 |
|---|---|---|
| 后端语言 | Go 1.24 | 单二进制，`CGO_ENABLED=0` 静态编译 |
| 后端框架 | Gin v1.10.0 | 只用 `gin.New()` + `Recovery()`，不用默认中间件 |
| ORM | GORM v1.31.1 + `gorm.io/driver/mysql` v1.6.0 | 只支持 MySQL |
| 鉴权 | `golang-jwt/jwt/v5` v5.2.2 | HS256，请求头 `x-token` |
| 密码 | `golang.org/x/crypto/bcrypt` | 复用 GVA 的 `sys_users.password` |
| 配置 | `gopkg.in/yaml.v3` | 读 `config.yaml` 的 mysql/jwt/system 三段 |
| 前端 | Vue 3.5 + Element Plus 2.13 + Vite 8 | **不用 vue-router、不用 pinia、不用 UnoCSS** |
| 前端请求 | axios 1.8 | 单实例 + 拦截器 |
| 前端拖拽 | sortablejs 1.15 | 只给服务宫格排序用 |
| 代理 | Python 3.10+ | **只用标准库**，不装任何第三方包 |
| 编排 | Docker Compose v2 | 两个服务：server + web |

**硬性约束（还原时必须遵守）**：

- Go 模块路径用 `github.com/yourname/k20mon/server`（可改，但所有 import 要同步改）。
- 前端**不要**引入 vue-router：三个页面靠 `<component :is>` 切换。
- 代理**不要**引入任何第三方 Python 包。
- 所有指标字段用指针（`*float64` / `*int64` / `*bool`），用来区分「值是 0」和「内核没这个节点」。
- 后端**启动时不允许因为 MySQL 连不上就退出**，必须后台重试。

---

## 3. 完整文件树

```
k20mon/
├── README.md
├── LICENSE                        MIT
├── Makefile                       help/server-*/web-*/agent-*/check/build/up/down/logs
├── .gitignore
├── .github/workflows/ci.yml       后端 + 前端 + 代理 + compose 四组检查
│
├── docs/
│   ├── AI-RESTORE.md              本文件
│   ├── ARCHITECTURE.md            架构与算法细节
│   ├── API.md                     全部 HTTP 接口契约
│   └── DEPLOY.md                  从零部署 / 排错 / 升级回滚
│
├── server/                        Go 后端
│   ├── go.mod                     module github.com/yourname/k20mon/server
│   ├── go.sum
│   ├── Dockerfile                 多阶段：golang:1.24-alpine → alpine:3.20
│   ├── .dockerignore
│   ├── main.go                    配置加载 / 路由 / JWT / DB 重试 / 响应封装
│   ├── collector/
│   │   ├── collector.go           procfs + sysfs 采集
│   │   └── collector_test.go      用假 root 目录测采集与换算
│   ├── model/
│   │   ├── metrics.go             指标结构 + MetricSample 表模型
│   │   ├── control.go             控制请求/响应结构
│   │   ├── services.go            服务目录表模型 + 默认种子
│   │   └── request/
│   │       ├── metrics.go         历史查询参数 + 区间解析
│   │       └── metrics_test.go
│   └── service/
│       ├── metrics.go             采样调度 / 当前快照 / 历史 / 统计 / SSE 订阅
│       ├── metrics_test.go
│       ├── battery_history.go     电池容量估算
│       ├── battery_history_test.go
│       ├── control.go             Unix socket 客户端 + 动作白名单
│       ├── services.go            服务目录 CRUD + DB 句柄 + 种子
│       └── services_test.go
│
├── web/                           Vue 3 前端
│   ├── package.json
│   ├── package-lock.json
│   ├── vite.config.js
│   ├── index.html
│   ├── nginx.conf                 静态托管 + /api 反代 + SSE 特例
│   ├── Dockerfile                 多阶段：node:22-alpine → nginx:alpine
│   ├── .dockerignore
│   ├── .env.development
│   ├── .env.production
│   ├── .gitignore
│   ├── tests/dashboard-refresh.test.js   纯 node 断言，无测试框架
│   └── src/
│       ├── main.js                入口：装 Element Plus、挂全局 axios 句柄
│       ├── App.vue                登录页 + 布局壳 + 三视图切换
│       ├── api/
│       │   ├── client.js          axios 实例 + 拦截器
│       │   └── auth.js            登录态存取 + login/getUserInfo
│       └── k20mon/
│           ├── api/metrics.js     全部业务接口封装
│           ├── utils/dashboard-cache.js   localStorage 读写容错
│           ├── utils/dashboard-refresh.js 去重 + summary 节流的刷新器
│           └── views/
│               ├── dashboard.vue  仪表盘
│               ├── operations.vue 操作中心
│               └── services.vue   服务宫格
│
└── deploy/
    ├── docker-compose.yml         server + web
    ├── .env.example
    ├── config.yaml.example        mysql / jwt / system 三段
    ├── services.json.example      服务宫格初始数据
    └── agent/
        ├── k20mon_agent.py        root 控制代理（标准库）
        ├── k20mon-agent.service   systemd unit
        ├── rate_sync.py           可选：外部定价任务控制
        ├── test_agent.py          纯函数单测
        └── test_rate_sync.py
```

---

## 4. 数据契约（必须逐字一致）

### 4.1 指标结构 `server/model/metrics.go`

```go
package model

import "time"

// Metrics 是一次完整采集的快照。所有数值字段都是指针：
// nil = 该内核节点不存在，*p = 0 表示节点存在且值为 0。
type Metrics struct {
	Timestamp time.Time      `json:"timestamp"`
	Battery   BatteryMetrics `json:"battery"`
	Charger   ChargerMetrics `json:"charger"`
	Network   NetworkMetrics `json:"network"`
	System    SystemMetrics  `json:"system"`
	Warnings  []string       `json:"warnings"`
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
	CPUUsagePercent *float64          `json:"cpuUsagePercent"`
	Load1           *float64          `json:"load1"`
	Load5           *float64          `json:"load5"`
	Load15          *float64          `json:"load15"`
	CPUCores        *int64            `json:"cpuCores"`
	FrequencyMHz    FrequencyMetrics  `json:"frequencyMhz"`
	Governors       []string          `json:"governors"`
	Temperatures    map[string]float64 `json:"temperatures"`
	HottestZone     string            `json:"hottestZone"`
	HottestC        *float64          `json:"hottestC"`
	Memory          MemoryMetrics     `json:"memory"`
	GPU             GPUMetrics        `json:"gpu"`
	UptimeSeconds   *int64            `json:"uptimeSeconds"`
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

// MetricSample 是落库表：payload 存完整 JSON，同时把图表要用的字段冗余成列。
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

func (MetricSample) TableName() string { return "k20mon_metric_samples" }
```

### 4.2 控制请求/响应 `server/model/control.go`

```go
package model

// ControlResponse 是 root 代理回包的扁平结构，各动作按需填其中几个字段。
type ControlResponse struct {
	OK           bool             `json:"ok"`
	Message      string           `json:"message,omitempty"`
	Error        string           `json:"error,omitempty"`
	Detail       string           `json:"detail,omitempty"`
	Data         map[string]any   `json:"data,omitempty"`
	Config       map[string]any   `json:"config,omitempty"`
	Tunnels      map[string]any   `json:"tunnels,omitempty"`
	Wifi         map[string]any   `json:"wifi,omitempty"`
	Cloudflare   map[string]any   `json:"cloudflare,omitempty"`
	RateSync     map[string]any   `json:"rate_sync,omitempty"`
	Interfaces   []map[string]any `json:"interfaces,omitempty"`
	Networks     []map[string]any `json:"networks,omitempty"`
	Services     []map[string]any `json:"services,omitempty"`
	Interface    string           `json:"interface,omitempty"`
	Mode         string           `json:"mode,omitempty"`
	Total        int              `json:"total,omitempty"`
	Online       int              `json:"online,omitempty"`
	Offline      int              `json:"offline,omitempty"`
	CheckedAt    float64          `json:"checked_at,omitempty"`
	Service      map[string]any   `json:"service,omitempty"`
	Capacity     *int64           `json:"capacity,omitempty"`
	CurrentMaxUA *int64           `json:"current_max_ua,omitempty"`
}

type ChargeGuardRequest struct {
	Enabled         *bool `json:"enabled"`
	LowThreshold    *int  `json:"low_threshold"`
	ResumeThreshold *int  `json:"resume_threshold"`
	LowCurrentUA    *int  `json:"low_current_ua"`
	NormalCurrentUA *int  `json:"normal_current_ua"`
}

type WifiConnectRequest struct {
	Interface string `json:"interface"`
	SSID      string `json:"ssid"`
	Password  string `json:"password"`
}

type WifiDisconnectRequest struct {
	Interface string `json:"interface"`
}

type FrpApplyRequest struct {
	Tunnels []map[string]any `json:"tunnels"`
}

type ServiceOwnerUpdateRequest struct {
	ID        string `json:"id"`
	OwnerType string `json:"ownerType"`
}

// ServiceConfigRequest 同时接受驼峰 ownerType 和下划线 owner_type，
// 因为前端两处都出现过；ownerType 优先。
type ServiceConfigRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Category        string `json:"category"`
	OwnerType       string `json:"ownerType"`
	OwnerTypeLegacy string `json:"owner_type"`
	URL             string `json:"url"`
	Enabled         *bool  `json:"enabled"`
}

type ServiceOrderRequest struct {
	IDs []string `json:"ids"`
}
```

### 4.3 服务目录 `server/model/services.go`

```go
package model

import "time"

type ServiceCatalog struct {
	ID          string    `gorm:"primaryKey;size:64" json:"id"`
	Name        string    `gorm:"size:120;not null" json:"name"`
	Description string    `gorm:"size:240" json:"description"`
	Category    string    `gorm:"size:60;not null" json:"category"`
	OwnerType   string    `gorm:"size:16;not null;index" json:"owner_type"`
	URL         string    `gorm:"size:2048;not null" json:"url"`
	Enabled     bool      `gorm:"not null;default:true;index" json:"enabled"`
	SortOrder   int       `gorm:"not null;default:0;index" json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (ServiceCatalog) TableName() string { return "k20mon_service_catalog" }

// 首次启动且表为空时写入的种子数据，URL 用占位值，由使用者自行改。
func DefaultServiceCatalog() []ServiceCatalog {
	rows := []ServiceCatalog{
		{ID: "k20mon", Name: "K20Mon", Description: "设备监控与操作中心", Category: "监控", OwnerType: "mine", URL: "http://127.0.0.1:8899", Enabled: true},
		{ID: "1panel", Name: "1Panel", Description: "服务器管理面板", Category: "管理", OwnerType: "mine", URL: "https://example.com/1panel", Enabled: false},
		{ID: "it-tools", Name: "IT Tools", Description: "常用开发与运维工具", Category: "工具", OwnerType: "mine", URL: "https://example.com/it-tools", Enabled: false},
	}
	for i := range rows {
		rows[i].SortOrder = i
	}
	return rows
}
```

### 4.4 历史查询 `server/model/request/metrics.go`

```go
package request

import (
	"errors"
	"time"
)

type MetricsHistoryRequest struct {
	From  string `form:"from" json:"from"`
	To    string `form:"to" json:"to"`
	Limit int    `form:"limit" json:"limit"`
}

// Range 把可选的 RFC3339 字符串解析成时间区间。
// 默认最近 24 小时；limit 默认 20000（5 秒采样约 17280 行/天），上限 600000。
func (r MetricsHistoryRequest) Range(now time.Time) (time.Time, time.Time, int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	to := now
	from := now.Add(-24 * time.Hour)
	var err error
	if r.To != "" {
		to, err = time.Parse(time.RFC3339, r.To)
		if err != nil {
			return time.Time{}, time.Time{}, 0, errors.New("to must be RFC3339")
		}
	}
	if r.From != "" {
		from, err = time.Parse(time.RFC3339, r.From)
		if err != nil {
			return time.Time{}, time.Time{}, 0, errors.New("from must be RFC3339")
		}
	}
	if from.After(to) {
		return time.Time{}, time.Time{}, 0, errors.New("from must not be after to")
	}
	limit := r.Limit
	if limit <= 0 {
		limit = 20_000
	}
	if limit > 600_000 {
		limit = 600_000
	}
	return from.UTC(), to.UTC(), limit, nil
}
```

---

## 5. 采集层实现规格 `server/collector/collector.go`

### 5.1 结构

```go
type Collector struct {
	root string // 可注入的文件系统根，默认 "/"，测试时传 t.TempDir()

	mu            sync.Mutex
	lastSampleAt  time.Time
	lastRXBytes   int64
	lastTXBytes   int64
	lastCPUIdle   int64
	lastCPUTotal  int64
	haveNetSample bool
	haveCPUSample bool

	thermalReadDisabled bool // thermal 读超时后熔断，避免每轮都卡
}

func New() *Collector                      { return NewWithRoot(string(filepath.Separator)) }
func NewWithRoot(root string) *Collector    // filepath.Clean(root)
func (c *Collector) Collect(ctx) (model.Metrics, error)       // 全量
func (c *Collector) CollectPower(ctx) (model.Metrics, error)   // 只读电池+充电器
```

### 5.2 设备路径常量

```go
const (
	batteryPath = "sys/class/power_supply/qcom-battery"
	chargerPath = "sys/class/power_supply/pm8150b-charger"
)
```

GPU devfreq 路径：`sys/devices/platform/soc@0/2c00000.gpu/devfreq/2c00000.gpu`，找不到时回退到 `sys/class/devfreq/*gpu*`，再回退到 `sys/class/devfreq/*`。

### 5.3 每个节点怎么读、怎么换算

读文件统一用 `read(path) (string, bool)`（TrimSpace），整数用 `readInt`（先 ParseInt，失败再 ParseFloat 截断）。
每个数值节点优先读独立文件，读不到再回退到同目录 `uevent` 里的 `POWER_SUPPLY_*` 键。

| 目标字段 | sysfs 文件 | uevent 回退键 | 换算 |
|---|---|---|---|
| `CapacityPercent` | `capacity` | `POWER_SUPPLY_CAPACITY` | 原值 |
| `VoltageV` | `voltage_now` | `POWER_SUPPLY_VOLTAGE_NOW` | ÷ 1e6（uV → V） |
| `CurrentMA` | `current_now` | `POWER_SUPPLY_CURRENT_NOW` | ÷ 1e3（uA → mA） |
| `PowerW` | 由电压电流算 | — | `voltageUV * abs(currentUA) / 1e12` |
| `TemperatureC` | `temp` | `POWER_SUPPLY_TEMP` | ÷ 10（十分之一度） |
| `ChargeNowUah` | `charge_now` | `POWER_SUPPLY_CHARGE_NOW` | 原值（uAh） |
| `ChargeFullUah` | `charge_full` | `POWER_SUPPLY_CHARGE_FULL` | 原值 |
| `ChargeFullDesignUah` | `charge_full_design` | `POWER_SUPPLY_CHARGE_FULL_DESIGN` | 原值 |
| `EnergyNowUwh` / `EnergyFullUwh` / `EnergyFullDesignUwh` | `energy_*` | `POWER_SUPPLY_ENERGY_*` | 原值（uWh） |
| `CycleCount` | `cycle_count` | `POWER_SUPPLY_CYCLE_COUNT` | 原值 |
| `HealthPercent` | 由 charge_full / charge_full_design × 100，缺则用 energy_full / energy_full_design × 100 | — | % |
| `ETAMinutes` | 见 5.5 | — | 分钟 |
| `Trickle` | `Status == "Charging" && abs(CurrentMA) < 50` | — | bool |

充电器侧：

| 目标字段 | 文件 | 换算 |
|---|---|---|
| `Status` / `Health` / `BatteryStatus` | `status` / `health` / 电池的 status | 原值 |
| `Online` | `online` → `!= 0` | bool 指针 |
| `VoltageV` | `voltage_now`，仅当 `0 < v < 20_000_000` 才采纳 | ÷ 1e6 |
| `CurrentMA` | `current_now` | ÷ 1e3 |
| `InputPowerW` | 电压 × 电流 ÷ 1000 | W |
| `CurrentMaxUA` | `current_max` | 原值；`<= 1_000_000` 时置 `Throttled=true`，`ThrottledReason = "input current limit is %dmA"` |
| `USBType` / `USBTypeRaw` | `usb_type`，方括号里的是当前生效值：` [PD]` → `PD` | `selectedUSBType()` |
| `TypeC` | 第一个匹配 `sys/class/power_supply/*typec*` 的目录 | `PDNegotiated = online && strings.HasPrefix(active, "PD")` |

### 5.4 网络与系统

- `interfaces()`：遍历 `sys/class/net/*`，每个取 `operstate`、`statistics/rx_bytes`、`statistics/tx_bytes`、`speed`。
- `defaultInterface()`：解析 `proc/net/route` 第二列（Destination）为 `00000000` 的行，取第一列；解析不到且 `root == "/"` 时再执行 `ip route show default` 找 `dev` 后的字段。
- 速率：`RXBPS = (rx_now - rx_prev) / elapsedSeconds`，首次采样不出速率（`haveNetSample`）。
- CPU 使用率：读 `proc/stat` 的 `cpu ` 行，累加所有字段为 total，`idle = field[3] + field[4]`；用两次采样的差值算 `(1 - idleDelta/totalDelta) * 100`，clamp 到 0..100；首次采样返回 0。
- 负载：`proc/loadavg` 前三个字段。
- 频率：遍历 `sys/devices/system/cpu/cpu[0-9]*/cpufreq`，取 `scaling_cur_freq`（kHz）÷ 1000 得 MHz，算平均/最小/最大；`scaling_governor` 去重后放进 `Governors`。
- 内存：`proc/meminfo` 转成 map，`MemTotal/MemAvailable/SwapTotal/SwapFree` 各 ÷ 1024 得 MB，`Used = Total - Available`，算百分比。
- 运行时长：`proc/uptime` 第一个字段取整。
- 温度：遍历 `sys/class/thermal/thermal_zone*`，读 `type` 和 `temp`（m℃ ÷ 1000 得 ℃）。**必须带超时与熔断**：单次读 200ms 超时、整轮预算 750ms，一旦超时或读失败就设 `thermalReadDisabled = true` 并停止本轮，后续轮次直接跳过。
- GPU：读 `cur_freq` / `min_freq` / `max_freq`（Hz ÷ 1e6 得 MHz）、`name`、`governor`、`load`（或 `gpu_busy_percentage` / `gpu_busy`，clamp 0..100）、`uevent` 里的 `DRIVER`、`of_node/compatible` 里的型号；温度从 thermal zone 里名字含 `gpu` 或 `kgsl` 的取。

### 5.5 两个派生算法

```go
// 充电时：输入功率 - 电池功率 = 整机功耗；放电/满电/未充电：直接等于电池功率
func estimateConsumptionW(status string, batteryPowerW, inputPowerW *float64) *float64

// 预计剩余时间：
//   Charging:    remainingAh = (100-capacity)/100 * designAh
//   Discharging: remainingAh = capacity/100 * designAh
//   minutes = remainingAh / (currentMA/1000) * 60
// 电流绝对值 <= 50mA 或 designAh <= 0 时返回 nil（涓流/数据不可靠）
func estimateETA(status string, capacity int64, currentMA float64, designUah int64) *int64
```

### 5.6 告警

采集不到关键节点时往 `Warnings` 里追加，不返回错误：
- `"battery status is unavailable"`（status 为空或 `Unknown`）
- `"charger status is unavailable"`（status 为 `Unknown` 且 online 为 nil）
- `"default network interface is unavailable"`
- `"procfs CPU metrics are unavailable"`（CPU 使用率和 load1 都 nil）

`Collect` 和 `CollectPower` 都要在开头和结束前检查 `ctx.Err()`，被取消就返回错误。

---

## 6. 服务层实现规格

### 6.1 `server/service/services.go`（DB 句柄 + 服务目录）

```go
var Catalog = NewServiceCatalogService()

// DB 用原子指针：可能长期为 nil（数据库没起来），连上后由后台循环挂上。
var db atomic.Pointer[gorm.DB]

var ErrStoreUnavailable = errors.New("k20mon database is unavailable")

func SetDB(value *gorm.DB)     // nil 表示断开
func conn() *gorm.DB           // 内部用
func Conn() *gorm.DB           // HTTP 层用，可能返回 nil
func storeAvailable() bool
```

服务目录方法（全部先取 `conn()`，为 nil 返回 `ErrStoreUnavailable`）：

- `List(ctx)`：`ORDER BY sort_order ASC, created_at ASC, id ASC`
- `Create(ctx,row)`：校验 → 取 `COALESCE(MAX(sort_order),-1)+1` → 插入 → `syncAgent`
- `Update(ctx,row)`：校验 → 读出旧记录保留 `CreatedAt`/`SortOrder` → `Save` → `syncAgent`
- `Reorder(ctx,ids)`：ID 正则校验 + 去重 → 事务内比对当前 ID 集合（数量不一致报"服务列表已变化，请刷新后重试排序"）→ 逐个更新 `sort_order` → `syncAgent`
- `UpdateOwner(ctx,id,ownerType)`：只允许 `mine` / `other`
- `Delete(ctx,id)`：`RowsAffected == 0` 时返回 `gorm.ErrRecordNotFound`
- `EnsureSeed(ctx)`：表为空时先尝试从代理拿 `services_status`，拿到就用代理的目录做种子，否则用 `DefaultServiceCatalog()`
- `syncAgent(ctx)`：把整表打包成 `[]map[string]any` 调 `services_sync`，3 秒超时，失败静默

校验规则（`validateServiceCatalog`）：
- ID 匹配 `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`
- Name 非空且 ≤ 120 字符
- Description ≤ 240，Category ≤ 60
- OwnerType ∈ {`mine`, `other`}
- URL 必须是 http/https 且 host 非空

### 6.2 `server/service/control.go`（socket 客户端）

```go
var Operations = NewOperationService()
var ErrControlUnavailable = errors.New("k20mon root control agent is unavailable")

// 白名单必须和 deploy/agent/k20mon_agent.py 的 control_action 完全一致
var allowedControlActions = map[string]struct{}{
	"charge_guard_get": {}, "charge_guard_set": {},
	"wifi_status": {}, "wifi_scan": {}, "wifi_connect": {}, "wifi_disconnect": {},
	"cloudflare_status": {},
	"rate_sync_status": {}, "rate_sync_config": {}, "rate_sync_run": {},
	"services_status": {}, "services_sync": {},
	"frp_config": {}, "frp_apply": {}, "frp_restart": {},
}
```

`ControlClient.socket()` **必须延迟解析**：`Operations` 是包级变量，在 `main()` 之前就初始化了，那时候还读不到配置文件里的 socket 路径。所以每次调用时先看环境变量 `K20M_CONTROL_SOCKET`，再看字段，最后回退 `/run/k20mon-status/control.sock`。

`Call` 流程：校验动作在白名单 → 序列化 `{"action":...,"data":...}` → `net.Dialer{Timeout: 35s}` 连 Unix socket → `SetDeadline(now+35s)` → 写一行 JSON（末尾 `\n`）→ `json.Decoder` 读一行响应 → 返回。

### 6.3 `server/service/metrics.go`

```go
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
```

- `Current(ctx)`：若最新快照年龄 ≤ 8 秒直接返回；否则调 `CollectPower` 只刷电池/充电器，**复用上一次的 System/Network**，然后 `publish`。
- `sample(ctx)`：`Collect` 全量 → **先 publish 再落库**（保证 MySQL 慢的时候 `/current` 依然快）→ 落库失败时追加 warning `"history persistence unavailable"` 并再 publish 一次。
- `publish(snapshot)`：写 `latest`，向所有订阅者非阻塞投递（`select` + `default`）。
- `Subscribe(ctx)`：建容量 1 的 channel，注册；ctx 结束自动注销；返回 `unsubscribe` 用 `sync.Once` 保证幂等。
- `StartSampler(ctx, interval)`：只启动一次（`samplerStarted` 守卫）；启动时立刻采一次，之后按 ticker 采；每小时清理一次超过保留期的数据。
- `History(ctx, query)`：`query.Range` 解析区间 → 按 `sampled_at DESC` 查 → 反序还原成时间正序 → 逐行 `json.Unmarshal(payload)`，单行解析失败就跳过。
- `Summary(ctx, query)`：`History` 的结果逐指标做 `summarize`（count/min/max/avg），再加 `batteryHistorySummary`。
- `retentionDuration()`：读 `K20M_RETENTION_H`（小时，默认 720）。

### 6.4 `server/service/battery_history.go`（电池实际容量估算）

这是全项目最需要小心的算法，**必须按下面逻辑实现**。

思路：电池驱动常常不提供真实满充容量（只有设计容量），但充放电过程中「电流积分 ÷ 电量百分比变化」可以反推实际容量。

```go
const (
	batteryHistoryMaxRows = 600_000
	batterySummaryTTL     = time.Minute
	batteryMaxSampleGap   = 30 * time.Second // 采样断档超过 30 秒就断开区间
)
```

1. `batteryHistorySummary`：TTL 1 分钟。缓存有效直接返回；否则**立即返回旧值**并起一个后台 goroutine 刷新（`batteryHistoryRefreshing` 防重入），刷新超时 2 分钟。**绝不能阻塞 HTTP 请求**，因为要扫几十万行。
2. `loadBatteryHistory`：只 `SELECT` 8 个列，保留期内的数据按 `sampled_at DESC` 取最多 60 万行，然后原地反序。
   MySQL 方言下对每列做 `COALESCE(列, JSON_EXTRACT(payload,'$.battery.xxx'))`，兼容早期只写了 payload 的老数据。
3. `deriveBatteryHistory(rows)`：
   - 遍历相邻两行，`elapsed = 当前时间 - 上一行时间`；`elapsed <= 0` 或 `> 30s` 视为断档，结束当前区间（`finishRun`）后 continue。
   - `direction`：状态含 `charging` → charge；`discharging` / `not charging` → discharge。
   - 区间容量 `amountUah`：两行都有 `currentMa` 时用梯形积分 `(|i_prev| + |i_cur|)/2 * hours * 1000`；否则若设计容量已知且百分比朝正确方向变化，用 `|Δpercent| * designUah / 100`，并置 `UsedDesignFallback = true`。
   - 累计：`chargedUah`、`dischargedUah`；放电时额外算 `dischargeEnergyWh += amountUah * voltageV / 1e6` 和 `dischargeHours`。
   - 整机功耗：`averageConsumptionPower` 优先用两行的 `consumptionW` 平均，放电/未充电时退回 `powerW` 平均；累加 `deviceEnergyWh`、`deviceEnergyHours`。
   - **容量估算区间**：只在方向合法（充电时百分比不降、放电时百分比不升）且方向没变时累计；区间要有 `startPercent`、`endPercent`、累计电流 `chargeUah`、以及「整段都有电流数据」标志 `completeCurrent`。区间结束时若 `hasCurrent && completeCurrent && |Δpercent| >= 5 && chargeUah > 0`，则 `estimate = chargeUah * 100 / |Δpercent|`，落在 `[100_000, 20_000_000]` uAh 才采纳。
   - 最终：多个区间估算取**中位数**（偶数个取中间两个平均）作为 `EstimatedFullUah`，`EstimatedHealthPercent = median / designUah * 100`。
   - `EquivalentCycles = dischargedUah / fullUah`，`fullUah` 优先用估算值，没有才用设计容量。
   - `EstimatedRemainingUah = fullUah * 最后一行的 capacityPercent / 100`。
   - `WindowHours = ceil(最后一行时间 - 第一行时间)`。
   - 用中位数而不是平均，是为了抗住个别区间因为采样抖动产生的离群值。

---

## 7. HTTP 接口契约 `server/main.go`

### 7.1 统一约定

- 业务响应一律 HTTP 200 + `{"code":0,"data":...,"msg":"..."}`；`code=0` 成功，`code=7` 业务失败（`data` 为 `{}`）。
- 鉴权失败走 HTTP 401 + `{"code":7,"data":null,"msg":"..."}`。
- 鉴权头 `x-token`，JWT HS256，校验时**必须**显式检查 `token.Method == jwt.SigningMethodHS256`，防止 alg 混淆。
- 响应 `Content-Type: application/json; charset=utf-8`。

### 7.2 路由表

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/health` | 否 | `{"status":"ok","service":"k20mon"}` |
| POST | `/base/login` | 否 | 用户名密码 → `{user,token,expiresAt}` |
| GET | `/user/getUserInfo` | 是 | 当前用户 |
| GET | `/k20mon/metrics/current` | 是 | 当前快照 |
| GET | `/k20mon/metrics/history` | 是 | 历史采样 |
| GET | `/k20mon/metrics/summary` | 是 | 区间统计 |
| GET | `/k20mon/metrics/stream` | 是 | SSE |
| GET | `/k20mon/operations/charge-guard` | 是 | 读充电守护 |
| PUT | `/k20mon/operations/charge-guard` | 是 | 写充电守护 |
| GET | `/k20mon/operations/wifi/status` | 是 | Wi-Fi 状态 |
| POST | `/k20mon/operations/wifi/scan` | 是 | 扫描（query `interface` 可选） |
| POST | `/k20mon/operations/wifi/connect` | 是 | `{interface,ssid,password}` |
| POST | `/k20mon/operations/wifi/disconnect` | 是 | `{interface}` |
| GET | `/k20mon/operations/cloudflare/status` | 是 | CF Tunnel 状态 |
| GET | `/k20mon/operations/rate-sync` | 是 | 定价任务状态 |
| PUT | `/k20mon/operations/rate-sync/config` | 是 | 定价配置（任意 JSON 对象） |
| POST | `/k20mon/operations/rate-sync/run` | 是 | 立即执行 |
| GET | `/k20mon/operations/frp/config` | 是 | 隧道列表 |
| PUT | `/k20mon/operations/frp/config` | 是 | `{tunnels:[...]}` |
| POST | `/k20mon/operations/frp/restart` | 是 | 重启 frpc |
| GET | `/k20mon/operations/services/status` | 是 | 服务探活 |
| PUT | `/k20mon/operations/services/owner` | 是 | 切换归属 |
| POST | `/k20mon/operations/services` | 是 | 新建 |
| PUT | `/k20mon/operations/services` | 是 | 修改 |
| POST | `/k20mon/operations/services/order` | 是 | `{ids:[...]}` 排序 |
| DELETE | `/k20mon/operations/services/:id` | 是 | 删除 |

### 7.3 关键实现细节

- **登录**：查 `sys_users` 表（复用 GVA 的表），`bcrypt.CompareHashAndPassword` 校验，`enable != 1` 视为禁用。数据库不可用时返回 `"数据库暂不可用，请稍后重试"`，**不能 500**。
- **SSE**：先设 `Content-Type: text/event-stream`、`Cache-Control: no-cache`、`Connection: keep-alive`；订阅后立刻推一次当前快照；之后循环 select ctx.Done / 新快照；每条写成 `event: metrics\ndata: {json}\n\n` 并 `Flush()`。
- **`callAction`**：把「可选地把请求体转成 map」+「调代理」+「按 `result.OK` 决定成功失败」封装成一个 `gin.HandlerFunc`，`ops.*` 全部复用它。
- **`bind[T]`**：泛型辅助，把强类型请求体转成代理需要的 map。
- **配置加载顺序**：环境变量优先 → `config.yaml` 兜底 → 必填项缺失才报错。`K20MON_MYSQL_DSN` 直接给 DSN 时完全跳过文件里的 mysql 段。
- **数据库重试**：`connectDatabase(dsn)` 起一个 goroutine 死循环，每 10 秒试一次 `gorm.Open` + `Ping` + `AutoMigrate`，成功就 `service.SetDB(db)` 并 `EnsureSeed` 后退出循环；失败打日志继续。**HTTP 服务必须在重试之前就启动**。

---

## 8. 控制代理实现规格 `deploy/agent/k20mon_agent.py`

### 8.1 运行模型

- 以 root 运行，`main()` 只做一件事：`control_server()`。
- socket 路径来自 `K20MON_CONTROL_SOCKET`，默认 `/run/k20mon-status/control.sock`。
- 权限：`os.chmod(socket, int(os.environ.get("K20MON_CONTROL_SOCKET_MODE", "0o660"), 8))`。默认 0660，需要让非 root 的后端连上时设 `0666`。
- 协议：客户端连上后写一行 JSON（末尾 `\n`），代理回一行 JSON（末尾 `\n`）。单次读取上限 128 KiB，超时 35 秒。
- 并发：`accept()` 后每个连接开一个线程处理，主循环不阻塞。
- 任何解析异常都回 `{"ok": false, "error": "请求格式错误: <异常类名>"}`，不能让进程挂掉。

### 8.2 动作分派表（必须和 Go 侧白名单一致）

| action | data | 返回 |
|---|---|---|
| `charge_guard_get` | `{}` | `{ok, config{enabled,low_threshold,resume_threshold,low_current_ua,normal_current_ua}, mode, capacity, current_max_ua}` |
| `charge_guard_set` | 上述 config 的部分或全部 | 同 get，外加 `message` |
| `wifi_status` | `{}` | `{ok, wifi{available,interface,state,connected,connection,ssid,ip,cidr,gateway,signal,frequency_mhz}}` |
| `wifi_scan` | `{interface?}` | `{ok, interface, networks[{ssid,in_use,signal,security,channel,frequency_mhz,interface}]}` |
| `wifi_connect` | `{interface?,ssid,password}` | `{ok, message}` |
| `wifi_disconnect` | `{interface?}` | `{ok, message}` |
| `cloudflare_status` | `{}` | `{ok, cloudflare{running,mode,config_file,token_configured,tunnel_id,ingress[],reason}}` |
| `frp_config` | `{}` | `{ok, tunnels{name:{name,type,local_ip,local_port,remote_port,status,endpoint}}}` |
| `frp_apply` | `{tunnels:[{name,local_ip,local_port,remote_port}]}` | `{ok, message, tunnels}` |
| `frp_restart` | `{}` | `{ok, message}` |
| `services_status` | `{services?}` | `{ok, checked_at, total, online, offline, services[]}` |
| `services_sync` | `{services:[...]}` | `{ok, message, total}` |
| `rate_sync_status` | `{}` | `{ok, rate_sync{...}}` |
| `rate_sync_config` | 配置对象 | 同 status |
| `rate_sync_run` | `{}` | `{ok, ...}` |
| 其他 | — | `{"ok": false, "error": "不支持的操作"}` |

`data` 不是对象时统一回 `{"ok": false, "error": "操作参数必须是对象"}`。

### 8.3 各动作的实现要点

**充电守护**（写 sysfs，是唯一会真正改动内核参数的路径）
- 配置文件：`K20MON_CHARGE_GUARD_CONFIG`，默认 `/etc/k20-charge-guard.conf`，格式 `KEY=VALUE` 五行。
- 状态文件：`K20MON_CHARGE_GUARD_STATE`，内容为 `low` / `normal` / `off`。
- 电流上限节点：`K20MON_CHARGE_CURRENT_MAX`，默认 `/sys/class/power_supply/pm8150b-charger/current_max`（uA）。
- 容量节点：`K20MON_CHARGE_CAPACITY`，默认 `/sys/class/power_supply/qcom-battery/capacity`。
- 校验：`50 <= resume_threshold < low_threshold <= 100`；两个电流值都在 `[50_000, 10_000_000]` uA。
- 写配置：先备份成 `<名字>.before-k20mon-<epoch>`，再写临时文件（`0o600`）+ `os.replace` 原子替换。
- **写完立刻应用一次**，不要等下一轮轮询：
  - `enabled == false` → 写 `normal_current_ua` 到 current_max，state 写 `off`
  - `enabled == true`：`capacity >= low_threshold` → 用低电流、state `low`；`capacity <= resume_threshold` → 用正常电流、state `normal`；否则沿用上一次 mode（`low` 就继续低电流）
- 默认值：`enabled=true, low_threshold=80, resume_threshold=65, low_current_ua=100000, normal_current_ua=3000000`。

**Wi-Fi**
- 设备选择：`K20MON_WIFI_DEVICE` 优先；否则 `nmcli -t -f GENERAL.DEVICE,GENERAL.TYPE,GENERAL.STATE device status`，挑 type 是 `wifi` 或 `802-11-wireless` 的，状态以 `100` 开头的优先；NetworkManager 不可用时退回扫 `/sys/class/net/*` 里 `wl`/`wlan`/`wlp`/`wld` 开头且 `operstate` 是 `up`/`unknown` 的；最后兜底 `"wld0"`。
- `nmcli -t` 的输出里冒号会被转义成 `\:`，必须用 `split_nmcli()` 按反斜杠转义规则切分。
- 状态：先 `nmcli device show`，解析不到 IP 时用 `ip -4 -o addr show dev <iface>` 和 `ip route show default dev <iface>` 兜底。
- 扫描：`nmcli ... --rescan yes`，按 `(in_use, -signal, ssid)` 排序，`(ssid, security)` 去重。

**frp**
- 配置文件位置不写死：`docker inspect -f '{{range .Mounts}}{{if eq .Destination "/etc/natfrp/frpc.ini"}}{{.Source}}{{end}}{{end}}' <容器名>` 动态取挂载源路径。
- 改写时**保留 `[common]` 段原样**，只替换每个隧道段的 `type/local_ip/local_port/remote_port`，其它键（插件配置等）原样保留。
- 隧道名必须匹配 `[A-Za-z0-9_.-]{1,32}` 且不能叫 `common`；端口 1..65535；local_ip 匹配 `[A-Za-z0-9_.:-]{1,64}`。
- 写前备份 `<path>.before-k20mon-<epoch>`。
- 重启：`docker restart <容器名>`，超时 30 秒。

**Cloudflare Tunnel**
- 读 `K20MON_CLOUDFLARED_CONFIG`（默认 `/etc/cloudflared/config.yml`），逐行解析出 `tunnel:` 和 `- hostname:` / `service:` 配对。
- 运行状态：`ps -eo args=` 里同时含 `cloudflared` 和 ` tunnel ` 视为在跑。
- `mode`：token 文件存在且非空 → `token`；否则有配置文件 → `config`；否则 `unknown`。

**服务探活**
- 目录文件 `K20MON_SERVICES_FILE`（默认 `/etc/k20mon/services.json`），是个 JSON 数组。
- 探测：`curl -L -sS -o /dev/null -w "%{http_code} %{time_total}" --connect-timeout 4 --max-time 8 <url>`。
- 判定：`http_code < 500` → `online`；有 code 但 ≥ 500 → `error`；没 code → `offline`。
- 用 `ThreadPoolExecutor(max_workers=min(8, len(enabled)))` 并发；停用的服务不进探测，直接标 `disabled`。

### 8.4 `rate_sync.py`（可选模块）

控制一个外部脚本 + systemd timer（`K20MON_RATE_SYNC_SCRIPT` / `K20MON_RATE_SYNC_TIMER`）。缺少这些外部文件时只是相关接口报错，**不能影响代理启动**。如果不想保留这个模块，删掉 `rate_sync.py` 和代理里三处 `rate_sync_*` 分支即可。

---

## 9. 前端实现规格

### 9.1 依赖（package.json 里只保留这些）

```json
{
  "dependencies": {
    "@element-plus/icons-vue": "^2.3.1",
    "axios": "^1.8.2",
    "element-plus": "^2.13.6",
    "sortablejs": "^1.15.3",
    "vue": "^3.5.31"
  },
  "devDependencies": {
    "@vitejs/plugin-vue": "^6.0.6",
    "vite": "^8.0.10"
  }
}
```

**不要**装 vue-router / pinia / echarts / unocss / sass —— 这套前端用不到。

### 9.2 `vite.config.js`

- `root` = 配置文件所在目录，`@` 别名指向 `src`。
- dev server 端口读 `VITE_DEV_PORT`（默认 8898），把 `VITE_BASE_API`（默认 `/api`）代理到 `VITE_BASE_PATH:VITE_SERVER_PORT`，并 rewrite 去掉前缀。
- build：`outDir=dist`、`emptyOutDir`、`target=es2015`、`input=index.html`，产物文件名 `assets/[name]-[hash].js|css`。

### 9.3 `src/main.js`

装 `element-plus/dist/index.css`、`ElementPlus`（中文 locale `zh-cn.mjs`），把 axios 实例挂到 `globalThis.__K20MON_REQUEST__`，再 `createApp(App).mount('#app')`。

### 9.4 `src/api/client.js`

- 单例 axios，超时 10 分钟。
- 请求拦截：注入 `x-token`、`x-user-id`，`baseURL` 默认 `/api`。
- 响应拦截：成功时直接返回 `response.data`（解掉 axios 的包装）；如果响应头有 `new-token` 就刷新本地 token；`401` 时清登录态并 `window.dispatchEvent(new CustomEvent('k20mon:session-expired'))`。

### 9.5 `src/api/auth.js`

`SESSION_KEY = 'k20mon.session.v1'`，`getSession` / `saveSession` / `clearSession`（都 try/catch，隐私模式下 localStorage 会抛错），以及 `login(data)` → `POST /base/login`、`getUserInfo()` → `GET /user/getUserInfo`。

### 9.6 `src/App.vue`

- 未登录显示登录面板（用户名默认 `admin`，密码框回车提交）。
- 已登录显示顶栏 + 左侧导航（仪表盘 / 操作中心 / 服务宫格）+ `<component :is="activeComponent">` 切换，**不用 vue-router**。
- 挂载时用本地 token 调 `getUserInfo` 校验登录态，失败就清掉。
- 监听 `k20mon:session-expired` 事件踢回登录页。
- 窄屏（≤720px）导航变成三列网格。

### 9.7 `src/k20mon/api/metrics.js`

所有接口封装成函数，统一经过一个 `requestPayload` 包装：调用 `globalThis.__K20MON_REQUEST__`，如果返回体有 `code`/`data` 就按 `code !== 0` 抛 `msg`，否则原样返回。

### 9.8 `src/k20mon/utils/dashboard-refresh.js`

```js
createDashboardRefresher({ getCurrentMetrics, getMetricsSummary, onCurrent, onSummary, onError,
                           summaryIntervalMs = 60_000, now = () => Date.now() })
```

返回一个 `refresh({ forceSummary })`：
- current 用 `currentInFlight` 守卫，**同一时刻只有一个在飞**，重复调用直接复用同一个 Promise；
- summary 额外受 `nextSummaryAt` 节流，默认 60 秒一次；
- 用 `Promise.all` 等所有在飞的请求；
- 单个请求失败只调 `onError(error, 'current'|'summary')`，不影响另一个。

配套 `dashboard-cache.js`：`readDashboardCache(storage,key)` / `writeDashboardCache(storage,key,value)`，全部 try/catch，storage 不可用时返回 null / 静默失败。

### 9.9 三个视图

- **dashboard.vue**：电池环、充放电功率、温度、健康度、容量估算、CPU/GPU 频率与温度、内存、网络速率、Wi-Fi 内网访问地址（可复制）。用 `createDashboardRefresher` 轮询 current（约 5 秒）和 summary（60 秒），并用 localStorage 缓存上一次数据，刷新页面先显示旧值再更新。
- **operations.vue**：充电守护（开关 + 阈值 + 电流）、Wi-Fi（状态条 / 扫描列表 / SSID 选择 / 密码 / 连接 / 断开）、Cloudflare Tunnel 状态、动态定价、frp 隧道编辑（JSON 文本框 + 应用 + 重启）。所有写操作前用 `ElMessageBox.confirm` 二次确认。
- **services.vue**：服务宫格，按 `owner_type` 分「我的 / 别人的」两个 tab，支持搜索、新增、编辑、启用停用、删除、切换归属、拖拽排序（`sortablejs`），每个卡片显示探活状态、HTTP 码、响应耗时。

### 9.10 `nginx.conf`

- `listen 8080`，`root /usr/share/nginx/html`，`index index.html`。
- `/`：`try_files $uri $uri/ /index.html`，加 `Cache-Control: no-store`（保证发布即生效）。
- `/assets/`：`expires 30d` + `Cache-Control: public, immutable`（文件名带 hash）。
- `/api/`：反代 `http://server:8888`，`rewrite ^/api/(.*)$ /$1 break`，连接超时 3s、读写超时 15s。
- **SSE 特例**：`location = /api/k20mon/metrics/stream` 单独放宽 `proxy_read_timeout`/`proxy_send_timeout` 到 1h，并 `proxy_buffering off`。

---

## 10. 构建与部署

### 10.1 `server/Dockerfile`

```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /src
RUN apk add --no-cache ca-certificates git
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -mod=mod -trimpath -ldflags="-s -w" -o /out/k20mon-server .

FROM ${RUNTIME_IMAGE:-alpine:3.20}
RUN apk add --no-cache ca-certificates tzdata wget
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=builder /out/k20mon-server /app/k20mon-server
EXPOSE 8888
ENTRYPOINT ["/app/k20mon-server"]
```

> 注意：**不要**加 `USER` 切到非 root。原因有两个，写进注释里：`config.yaml` 里有 MySQL 密码通常是 0600，非 root 读不到；root 代理创建的 `control.sock` 是 `root:root 0660`，非 root 进程连不上。要收紧就按 Dockerfile 注释里的方案改权限和 `K20MON_CONTROL_SOCKET_MODE`。

### 10.2 `web/Dockerfile`

```dockerfile
FROM node:22-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci --no-audit --no-fund || npm install --no-audit --no-fund
COPY . .
RUN npm run build

FROM ${RUNTIME_IMAGE:-nginx:alpine}
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist/ /usr/share/nginx/html/
EXPOSE 8080
CMD ["nginx", "-g", "daemon off;"]
```

### 10.3 `deploy/docker-compose.yml`

两个服务：

**server**
- build `../server`，args `GOPROXY`（默认 `https://proxy.golang.org,direct`）、`RUNTIME_IMAGE`（默认 `alpine:3.20`）
- ports `${K20MON_SERVER_PORT:-18888}:8888`
- volumes：`${K20MON_CONFIG_PATH:-./config.yaml}:/etc/k20mon/config.yaml:ro`、`/sys:/sys:ro`、`/proc:/proc:ro`、`${K20MON_CONTROL_DIR:-/run/k20mon-status}:/run/k20mon-status`
- env：`K20M_INTERVAL=5`、`K20M_RETENTION_H=720`、`K20M_CONTROL_SOCKET=/run/k20mon-status/control.sock`、`K20MON_CONFIG_PATH=/etc/k20mon/config.yaml`、`TZ=Asia/Shanghai`
- healthcheck：`wget -qO- http://127.0.0.1:8888/health`，15s 间隔 / 5s 超时 / 5 次重试 / 20s 启动宽限

**web**
- build `../web`，args `RUNTIME_IMAGE`（默认 `nginx:alpine`）
- ports `${K20MON_WEB_PORT:-8899}:8080`
- `depends_on: server: condition: service_healthy`

### 10.4 部署步骤

```bash
# 1. 建库建表建管理员
mysql -uroot -p -e "CREATE DATABASE IF NOT EXISTS k20mon DEFAULT CHARSET utf8mb4;
  CREATE USER IF NOT EXISTS 'k20mon'@'%' IDENTIFIED BY '换成你的密码';
  GRANT ALL ON k20mon.* TO 'k20mon'@'%'; FLUSH PRIVILEGES;"

mysql -uroot -p k20mon <<'SQL'
CREATE TABLE IF NOT EXISTS sys_users (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  uuid VARCHAR(191) NOT NULL DEFAULT '',
  username VARCHAR(191) NOT NULL UNIQUE,
  password VARCHAR(255) NOT NULL,
  nick_name VARCHAR(191) NOT NULL DEFAULT '',
  header_img VARCHAR(255) NOT NULL DEFAULT '',
  authority_id BIGINT UNSIGNED NOT NULL DEFAULT 888,
  phone VARCHAR(32) NOT NULL DEFAULT '',
  email VARCHAR(191) NOT NULL DEFAULT '',
  enable INT NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
SQL

# 2. 生成 bcrypt 并插入管理员
python3 -c "import bcrypt;print(bcrypt.hashpw(b'你的密码', bcrypt.gensalt()).decode())"
# 或者用 htpasswd -bnBC 10 "" '你的密码' | tr -d ':\n'
mysql -uroot -p k20mon -e "INSERT INTO sys_users (uuid,username,password,nick_name,authority_id,enable)
  VALUES (UUID(),'admin','<上面生成的 hash>','管理员',888,1);"

# 3. 配置
cp deploy/.env.example deploy/.env
cp deploy/config.yaml.example deploy/config.yaml
# 编辑 config.yaml：mysql 的 path/port/db-name/username/password、jwt.signing-key

# 4. 装 root 代理
sudo install -d -m 0755 /usr/local/libexec /etc/k20mon /run/k20mon-status
sudo install -m 0755 deploy/agent/k20mon_agent.py /usr/local/libexec/
sudo install -m 0644 deploy/agent/rate_sync.py    /usr/local/libexec/
sudo install -m 0644 deploy/agent/k20mon-agent.service /etc/systemd/system/
sudo install -m 0644 deploy/services.json.example /etc/k20mon/services.json
sudo systemctl daemon-reload
sudo systemctl enable --now k20mon-agent
ls -l /run/k20mon-status/control.sock      # 应该能看到 socket

# 5. 起服务
cd deploy && docker compose up -d --build

# 6. 验证
curl -s http://127.0.0.1:18888/health          # {"status":"ok",...}
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8899/
curl -s http://127.0.0.1:8899/api/health
```

**MySQL 地址怎么填**（最容易踩的坑）：容器里的 `127.0.0.1` 是容器自己。要么填宿主机的 docker 网关 IP：

```bash
docker network inspect <compose项目名>_default --format '{{(index .IPAM.Config 0).Gateway}}'
# 通常是 172.x.0.1
```

要么在 compose 里给 server 加 `extra_hosts: ["host.docker.internal:host-gateway"]`，然后 config 里写 `host.docker.internal`。

---

## 11. 验收清单

还原完成后，下面每一条都必须成立：

**后端**
- [ ] `cd server && gofmt -l .` 输出为空
- [ ] `cd server && go vet ./...` 无输出
- [ ] `cd server && go test ./...` 全绿（collector / model/request / service 三个包都有测试）
- [ ] `CGO_ENABLED=0 go build -o k20mon-server .` 成功
- [ ] MySQL 故意填错地址启动，进程**不退出**，日志每 10 秒打印一次 `K20Mon database unavailable (attempt N)`
- [ ] 此时 `curl /health` 仍返回 200
- [ ] 此时 `POST /base/login` 返回 `{"code":7,"msg":"数据库暂不可用，请稍后重试"}`
- [ ] MySQL 正确时，`/k20mon/metrics/current` 能返回电池/充电器/网络/系统四段真实数据
- [ ] 不带 `x-token` 访问任意业务接口返回 HTTP 401
- [ ] 用错误 alg 的 JWT（例如 `alg:none`）访问返回 401

**前端**
- [ ] `cd web && npm run test` 4 个用例全过
- [ ] `cd web && npm run build` 产出 `dist/index.html` + `dist/assets/*.js|css`
- [ ] 产物里不含 vue-router / echarts / unocss 的代码

**代理**
- [ ] `cd deploy/agent && python3 -m unittest test_agent test_rate_sync -v` 全过
- [ ] 启动后 socket 文件存在且权限符合预期
- [ ] 每个白名单动作都能返回结构正确的 JSON
- [ ] 发非法 JSON → `{"ok":false,"error":"请求格式错误: JSONDecodeError"}`
- [ ] 发未知名动作 → `{"ok":false,"error":"不支持的操作"}`
- [ ] 5 个并发请求全部有响应（不会因为串行 accept 卡死）

**整体**
- [ ] `cd deploy && docker compose config` 无报错
- [ ] `docker compose up -d --build` 后 `server` 变 healthy，`web` 起来
- [ ] `curl 127.0.0.1:8899/` 返回 200
- [ ] `curl 127.0.0.1:8899/api/health` 返回 200
- [ ] 浏览器登录后三个页面都能加载，仪表盘有实时数据

---

## 12. 建议的还原顺序

分 6 步，每步都能独立验证，别一口气全写完再调。

1. **骨架**：建目录、`go.mod`、`package.json`、`.gitignore`、`LICENSE`、`Makefile`。
2. **后端只读链路**：`model/metrics.go` → `collector/collector.go` + 测试 → `service/metrics.go`（先不做 battery_history）→ `main.go`（路由 + JWT + `/health` + `/current`）。验证：`go test` 绿，容器里能拿到真实指标。
3. **存储与历史**：`services.go`（DB 句柄 + 重试）→ `metrics.go` 的 History/Summary → `battery_history.go` + 测试。验证：MySQL 停掉后端不挂。
4. **控制通道**：`model/control.go` → `service/control.go` → 代理的 `control_server` + 三个动作（charge_guard / wifi / services）→ 逐个打通。
5. **前端**：`api/` → `App.vue` → `dashboard.vue` → `operations.vue` → `services.vue` → `nginx.conf`。验证：`npm run build` 出包，nginx 起得来，能登录。
6. **部署与文档**：compose、Dockerfile、systemd unit、`.env.example`、`config.yaml.example`、`README`、`docs/*`。

---

## 13. 还原时最容易犯的错

1. **加了 `USER 10001` 到后端镜像** → 读不到 0600 的 config.yaml、连不上 0660 的 socket，表现为崩溃循环或"root control agent is unavailable"。
2. **MySQL 连不上就 panic** → 手机重启后数据库比后端起得慢，后端会无限重启。必须后台重试。
3. **`Operations` 初始化时就去读 socket 路径** → 那时配置文件还没解析，永远连默认路径。必须延迟解析。
4. **前端引入 vue-router** → 多出一个没必要的依赖，且和 `<component :is>` 切换冲突。
5. **用平均而不是中位数算电池容量** → 个别异常区间把结果拉飞。
6. **`batteryHistorySummary` 同步扫全表** → 几十万行查询阻塞 HTTP，页面转圈。必须 TTL 缓存 + 后台刷新。
7. **`Collect` 里不加 thermal 超时** → 某个 thermal zone 卡住会让整个 `/current` 超时。必须 200ms 单次 + 750ms 整轮 + 熔断。
8. **SSE 走默认 nginx 超时** → 60 秒后连接被断，页面要不停重连。
9. **忘了 `split_nmcli` 处理转义** → SSID 里带冒号时解析错位。
10. **`collectBattery` 直接用 `voltage_now * current_now`** → 单位是 uV × uA = pW，要除以 1e12 才是 W。

---

## 14. 一句话总结给 AI

> 用 Go 写一个只读采集 + HTTP 的后端，用 Vue 3 写一个三页面的前端，用 Python 写一个只监听 Unix socket 的 root 白名单代理，三者用 Docker Compose 串起来；所有特权写操作只能经过代理，所有指标字段用指针表达"不可用"，后端在 MySQL 挂掉时必须继续活着并重连。
