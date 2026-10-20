# HTTP API 参考

## 通用约定

| 项 | 值 |
|---|---|
| Base URL（生产，经 nginx） | `http://<设备IP>:8899/api` |
| Base URL（直连后端） | `http://<设备IP>:18888` |
| 鉴权头 | `x-token: <JWT>` |
| Content-Type | `application/json; charset=utf-8` |
| 超时建议 | 普通接口 15s，控制类 35s，SSE 长连接 |

### 统一响应包

成功：

```json
{"code": 0, "data": { }, "msg": "成功"}
```

业务失败（HTTP 仍是 200）：

```json
{"code": 7, "data": {}, "msg": "服务名称不能为空且不能超过 120 个字符"}
```

鉴权失败（HTTP 401）：

```json
{"code": 7, "data": null, "msg": "未登录或非法访问，请登录"}
```

> 判定成功的唯一标准是 `code === 0`。不要用 HTTP 状态码判断业务结果。

### 鉴权

`POST /base/login` 返回的 `token` 放进 `x-token` 头。JWT HS256，默认 7 天过期（`jwt.expires-time` 或 `K20MON_JWT_EXPIRES`）。

Token payload：

```json
{
  "userId": 1, "uuid": "9505878d-...", "username": "admin",
  "nickName": "管理员", "authorityId": 888,
  "iss": "k20mon", "sub": "1", "iat": 1792470000, "nbf": 1792469999, "exp": 1793074800
}
```

### 接口总表

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/health` | 否 | 健康检查 |
| POST | `/base/login` | 否 | 登录 |
| GET | `/user/getUserInfo` | 是 | 当前用户信息 |
| GET | `/k20mon/metrics/current` | 是 | 当前指标快照 |
| GET | `/k20mon/metrics/history` | 是 | 历史采样 |
| GET | `/k20mon/metrics/summary` | 是 | 区间统计 |
| GET | `/k20mon/metrics/stream` | 是 | 实时推送（SSE） |
| GET | `/k20mon/operations/charge-guard` | 是 | 读充电守护配置 |
| PUT | `/k20mon/operations/charge-guard` | 是 | 写充电守护配置 |
| GET | `/k20mon/operations/wifi/status` | 是 | Wi-Fi 状态 |
| POST | `/k20mon/operations/wifi/scan` | 是 | 扫描周边 Wi-Fi |
| POST | `/k20mon/operations/wifi/connect` | 是 | 连接 Wi-Fi |
| POST | `/k20mon/operations/wifi/disconnect` | 是 | 断开 Wi-Fi |
| GET | `/k20mon/operations/cloudflare/status` | 是 | Cloudflare Tunnel 状态 |
| GET | `/k20mon/operations/rate-sync` | 是 | 定价任务状态 |
| PUT | `/k20mon/operations/rate-sync/config` | 是 | 写定价配置 |
| POST | `/k20mon/operations/rate-sync/run` | 是 | 立即执行一次 |
| GET | `/k20mon/operations/frp/config` | 是 | 读 frp 隧道 |
| PUT | `/k20mon/operations/frp/config` | 是 | 写 frp 隧道 |
| POST | `/k20mon/operations/frp/restart` | 是 | 重启 frpc |
| GET | `/k20mon/operations/services/status` | 是 | 服务探活 |
| PUT | `/k20mon/operations/services/owner` | 是 | 切换服务归属 |
| POST | `/k20mon/operations/services` | 是 | 新建服务 |
| PUT | `/k20mon/operations/services` | 是 | 修改服务 |
| POST | `/k20mon/operations/services/order` | 是 | 保存排序 |
| DELETE | `/k20mon/operations/services/:id` | 是 | 删除服务 |

---

## 健康检查

### `GET /health`

不需要鉴权，给 Docker healthcheck 用。

```bash
curl http://127.0.0.1:18888/health
```

```json
{"status": "ok", "service": "k20mon"}
```

> 注意：这个接口**不检查数据库**。后端在 MySQL 挂掉时依然返回 200，数据库状态看日志。

---

## 登录

### `POST /base/login`

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `username` | string | 是 | 对应 `sys_users.username` |
| `password` | string | 是 | 明文，服务端用 bcrypt 比对 |

```bash
curl -X POST http://127.0.0.1:18888/base/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"你的密码"}'
```

成功：

```json
{
  "code": 0,
  "data": {
    "user": {
      "ID": 1,
      "uuid": "9505878d-7e4e-42c0-99aa-0faaa34284bd",
      "userName": "admin",
      "nickName": "管理员",
      "headerImg": "",
      "authorityId": 888,
      "phone": "",
      "email": "",
      "enable": 1
    },
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9....",
    "expiresAt": 1793074800000
  },
  "msg": "登录成功"
}
```

失败：

```json
{"code": 7, "data": {}, "msg": "用户名不存在或者密码错误"}
{"code": 7, "data": {}, "msg": "用户名和密码不能为空"}
{"code": 7, "data": {}, "msg": "数据库暂不可用，请稍后重试"}
```

> `password` 字段永远不出现在响应里（`json:"-"`）。

---

## 用户

### `GET /user/getUserInfo`

```bash
curl http://127.0.0.1:18888/user/getUserInfo -H "x-token: $TOKEN"
```

```json
{"code": 0, "data": {"userInfo": {"ID": 1, "userName": "admin", "nickName": "管理员", "authorityId": 888, "enable": 1}}, "msg": "获取成功"}
```

用户不存在时返回 HTTP 401 + `{"code":7,"data":null,"msg":"用户不存在"}`。

---

## 指标

### `GET /k20mon/metrics/current`

当前快照。电池/充电器是实时的，`system` / `network` 来自最近一次全量采样（最多 `K20M_INTERVAL` 秒旧）。

```bash
curl http://127.0.0.1:18888/k20mon/metrics/current -H "x-token: $TOKEN"
```

```json
{
  "code": 0,
  "data": {
    "timestamp": "2026-10-20T04:47:20.214962373Z",
    "battery": {
      "capacityPercent": 66,
      "status": "Not charging",
      "voltageV": 3.970692,
      "currentMa": 0,
      "powerW": 0,
      "temperatureC": 31,
      "chargeNowUah": null,
      "chargeFullUah": null,
      "chargeFullDesignUah": null,
      "energyNowUwh": null,
      "energyFullUwh": null,
      "energyFullDesignUwh": null,
      "consumptionW": 0,
      "healthPercent": null,
      "cycleCount": null,
      "etaMinutes": null,
      "trickle": false
    },
    "charger": {
      "status": "Not charging",
      "online": true,
      "health": "Good",
      "batteryStatus": "Not charging",
      "usbType": "SDP",
      "usbTypeRaw": "SDP",
      "voltageV": 5.1,
      "currentMa": 0,
      "inputPowerW": 0,
      "currentMaxUa": 725000,
      "pdNegotiated": false,
      "throttled": true,
      "throttledReason": "input current limit is 725mA",
      "typeC": null
    },
    "network": {
      "defaultInterface": "wld0",
      "rxBytes": 12345678,
      "txBytes": 2345678,
      "rxBps": 1024.5,
      "txBps": 256.25,
      "interfaces": [
        {"name": "wld0", "state": "up", "rxBytes": 12345678, "txBytes": 2345678, "speedMbps": null}
      ]
    },
    "system": {
      "cpuUsagePercent": 12.5,
      "load1": 0.8,
      "load5": 0.6,
      "load15": 0.5,
      "cpuCores": 8,
      "frequencyMhz": {"averageMhz": 1200.5, "minimumMhz": 576, "maximumMhz": 2841},
      "governors": ["schedutil"],
      "temperatures": {"cpu-0-0-usr": 42.5, "gpu-0-0": 40.0},
      "hottestZone": "cpu-0-0-usr",
      "hottestC": 42.5,
      "memory": {"totalMb": 7418, "usedMb": 3959, "availableMb": 3458, "usedPercent": 53.4, "swapTotalMb": 6143, "swapUsedMb": 0, "swapUsedPercent": 0},
      "gpu": {
        "available": true,
        "model": "qcom,adreno-640.1",
        "driver": "kgsl",
        "device": "2c00000.gpu",
        "governor": "msm-adreno-tz",
        "currentFrequencyMhz": 257,
        "minimumFrequencyMhz": 180,
        "maximumFrequencyMhz": 587,
        "utilizationPercent": 8,
        "temperatureC": 40
      },
      "uptimeSeconds": 345678
    },
    "warnings": []
  },
  "msg": "成功"
}
```

#### 字段说明

**battery**

| 字段 | 类型 | 单位 | 为 null 的条件 |
|---|---|---|---|
| `capacityPercent` | int64 | % | 内核无 `capacity` 节点 |
| `status` | string | — | 无节点时为 `"Unknown"` |
| `voltageV` | float64 | V | 无 `voltage_now` |
| `currentMa` | float64 | mA | 无 `current_now`；充电时为正，放电为负 |
| `powerW` | float64 | W | 无 `voltage_now` 或 `current_now` |
| `temperatureC` | float64 | ℃ | 无 `temp` |
| `chargeNowUah` | int64 | µAh | 驱动只提供 `energy_*` |
| `chargeFullUah` | int64 | µAh | 同上 |
| `chargeFullDesignUah` | int64 | µAh | 同上 |
| `energyNowUwh` / `energyFullUwh` / `energyFullDesignUwh` | int64 | µWh | 驱动只提供 `charge_*` |
| `consumptionW` | float64 | W | 充电时缺输入功率，或状态无法判断 |
| `healthPercent` | float64 | % | 两个容量字段都不全 |
| `cycleCount` | int64 | 次 | 无 `cycle_count` |
| `etaMinutes` | int64 | 分钟 | 电流 ≤ 50mA，或状态不是充/放电，或设计容量为 0 |
| `trickle` | bool | — | 充电中且电流 < 50mA 时为 true |

**charger**

| 字段 | 类型 | 说明 |
|---|---|---|
| `status` / `health` | string | sysfs 原值 |
| `online` | bool | `online != 0`；无节点时 null |
| `usbType` | string | 方括号里当前生效的值，如 ` [PD]` → `PD` |
| `usbTypeRaw` | string | 原始字符串 |
| `voltageV` / `currentMa` / `inputPowerW` | float64 | 输入侧电压/电流/功率 |
| `currentMaxUa` | int64 | 当前限流值（µA） |
| `pdNegotiated` | bool | Type-C 口在线且生效类型以 `PD` 开头 |
| `throttled` | bool | `currentMaxUa <= 1_000_000` 时为 true |
| `throttledReason` | string | 如 `input current limit is 725mA` |
| `typeC` | object | 找到 `*typec*` 目录时才有 |

**network**

| 字段 | 说明 |
|---|---|
| `defaultInterface` | 默认路由网卡名，探测不到为空串 |
| `rxBytes` / `txBytes` | 该网卡累计字节（不是速率） |
| `rxBps` / `txBps` | 两次采样间的字节速率，首次采样为 null |
| `interfaces[].speedMbps` | sysfs `speed`，无线网卡通常没有 |

**system**

| 字段 | 说明 |
|---|---|
| `cpuUsagePercent` | 两次 `/proc/stat` 差值算出的使用率，首次为 0 |
| `load1/5/15` | `/proc/loadavg` |
| `frequencyMhz` | 所有 CPU 核 `scaling_cur_freq` 的 平均/最小/最大 |
| `governors` | 去重后的调速器名字 |
| `temperatures` | zone 名 → ℃ |
| `hottestZone` / `hottestC` | 最热的那一个 |
| `memory.*` | `/proc/meminfo`，单位 MB |
| `gpu.*` | 见上；`available: false` 时其余字段为空 |
| `uptimeSeconds` | `/proc/uptime` |

**warnings**：采集不到关键节点时的提示字符串数组，例如 `["battery status is unavailable"]`。

---

### `GET /k20mon/metrics/history`

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `from` | string | 否 | RFC3339，默认 `to - 24h` |
| `to` | string | 否 | RFC3339，默认现在 |
| `limit` | int | 否 | 默认 20000，上限 600000 |

返回按时间**正序**排列的完整快照数组。

```bash
curl "http://127.0.0.1:18888/k20mon/metrics/history?from=2026-10-19T00:00:00Z&to=2026-10-20T00:00:00Z&limit=1000" \
  -H "x-token: $TOKEN"
```

```json
{"code": 0, "data": [ { "timestamp": "...", "battery": {...}, "charger": {...}, "network": {...}, "system": {...}, "warnings": [] } ], "msg": "成功"}
```

参数错误：

```json
{"code": 7, "data": {}, "msg": "from must be RFC3339"}
{"code": 7, "data": {}, "msg": "from must not be after to"}
```

数据库不可用：

```json
{"code": 7, "data": {}, "msg": "k20mon database is unavailable"}
```

---

### `GET /k20mon/metrics/summary`

参数同 `history`。返回区间内各指标的 min/max/avg，外加电池容量估算。

```bash
curl "http://127.0.0.1:18888/k20mon/metrics/summary?limit=1000" -H "x-token: $TOKEN"
```

```json
{
  "code": 0,
  "data": {
    "from": "2026-10-19T04:47:20Z",
    "to": "2026-10-20T04:47:20Z",
    "sampleCount": 1000,
    "batteryHistory": {
      "windowHours": 24,
      "sampleCount": 17280,
      "chargeAddedUah": 1250000,
      "dischargeUsedUah": 980000,
      "dischargeEnergyWh": 3.85,
      "averageConsumptionW": 0.42,
      "estimatedFullUah": 3900000,
      "estimatedHealthPercent": 97.5,
      "estimatedRemainingUah": 2574000,
      "equivalentCycles": 0.25,
      "capacityEstimateSamples": 6,
      "usedDesignFallback": false
    },
    "batteryCapacity": {"count": 1000, "min": 55, "max": 78, "avg": 66.4},
    "batteryPowerW": {"count": 800, "min": 0, "max": 6.2, "avg": 1.8},
    "batteryConsumptionW": {"count": 700, "min": 0.2, "max": 1.1, "avg": 0.45},
    "batteryTemperatureC": {"count": 1000, "min": 28, "max": 38, "avg": 31.2},
    "cpuUsagePercent": {"count": 1000, "min": 3.1, "max": 62.5, "avg": 14.8},
    "hottestTemperatureC": {"count": 1000, "min": 35, "max": 55, "avg": 41.5},
    "memoryUsedPercent": {"count": 1000, "min": 48.2, "max": 61.0, "avg": 53.4},
    "networkRxBps": {"count": 999, "min": 0, "max": 125000, "avg": 4200},
    "networkTxBps": {"count": 999, "min": 0, "max": 80000, "avg": 1800}
  },
  "msg": "成功"
}
```

> `batteryHistory` 有 1 分钟缓存，第一次调用可能返回全 null，下一次就有值。这是设计如此——它要扫几十万行，不能阻塞请求。

---

### `GET /k20mon/metrics/stream`（SSE）

服务端推送。每条消息：

```text
event: metrics
data: {"timestamp":"...","battery":{...},"charger":{...},"network":{...},"system":{...},"warnings":[]}

```

连上后立刻推一次当前快照，之后每次采样（默认 5 秒）推一次。

```bash
curl -N http://127.0.0.1:18888/k20mon/metrics/stream -H "x-token: $TOKEN"
```

前端接法：

```js
const source = new EventSource('/api/k20mon/metrics/stream')  // 注意：EventSource 不能加自定义头
```

> **坑**：`EventSource` 不支持自定义请求头，所以 `x-token` 传不进去。当前前端没有用 SSE，而是走 `/current` 轮询。如果要用 SSE，需要改成把 token 放 query（`?token=...`）或改用 `fetch` + `ReadableStream` 手动解析。

nginx 侧对这个路径单独放宽了超时并关掉 buffering，见 `web/nginx.conf` 里的 `location = /api/k20mon/metrics/stream`。

---

## 操作中心

以下所有接口最终都转发给 root 代理。代理不可用时统一返回：

```json
{"code": 7, "data": {}, "msg": "k20mon root control agent is unavailable: dial unix /run/k20mon-status/control.sock: connect: no such file or directory"}
```

### `GET /k20mon/operations/charge-guard`

```bash
curl http://127.0.0.1:18888/k20mon/operations/charge-guard -H "x-token: $TOKEN"
```

```json
{
  "code": 0,
  "data": {
    "ok": true,
    "config": {
      "enabled": true,
      "low_threshold": 80,
      "resume_threshold": 65,
      "low_current_ua": 100000,
      "normal_current_ua": 3000000
    },
    "mode": "normal",
    "capacity": 66,
    "current_max_ua": 725000
  },
  "msg": "成功"
}
```

| 字段 | 说明 |
|---|---|
| `config.enabled` | 是否启用充电守护 |
| `config.low_threshold` | 电量达到这个百分比就切低电流 |
| `config.resume_threshold` | 降到这个百分比才恢复正常电流 |
| `config.low_current_ua` | 低电流档（µA） |
| `config.normal_current_ua` | 正常电流档（µA） |
| `mode` | `low` / `normal` / `off` / `unknown` |
| `capacity` | 当前电量，读不到为 null |
| `current_max_ua` | 当前 sysfs 里的限流值 |

### `PUT /k20mon/operations/charge-guard`

所有字段可选，只传要改的。

| 参数 | 类型 | 约束 |
|---|---|---|
| `enabled` | bool | — |
| `low_threshold` | int | 51..100 |
| `resume_threshold` | int | 50..99，且必须 < `low_threshold` |
| `low_current_ua` | int | 50000..10000000 |
| `normal_current_ua` | int | 50000..10000000 |

```bash
curl -X PUT http://127.0.0.1:18888/k20mon/operations/charge-guard \
  -H "x-token: $TOKEN" -H 'Content-Type: application/json' \
  -d '{"enabled":true,"low_threshold":80,"resume_threshold":65,"low_current_ua":100000,"normal_current_ua":3000000}'
```

```json
{"code": 0, "data": {"ok": true, "message": "充电限制配置已保存并立即应用", "config": {...}, "mode": "normal", "capacity": 66, "current_max_ua": 3000000}, "msg": "成功"}
```

校验失败：

```json
{"code": 7, "data": {}, "msg": "阈值需满足 50 <= 恢复阈值 < 限制阈值 <= 100"}
{"code": 7, "data": {}, "msg": "低电流需在 50mA 到 10A 之间"}
```

> 这个接口**真的会改 sysfs**（`current_max`），保存后立即生效。

### `GET /k20mon/operations/wifi/status`

```json
{
  "code": 0,
  "data": {
    "ok": true,
    "wifi": {
      "available": true,
      "interface": "wld0",
      "state": "connected",
      "connected": true,
      "connection": "PDCN_5G",
      "ssid": "PDCN_5G",
      "ip": "192.168.1.2",
      "cidr": "192.168.1.2/24",
      "gateway": "192.168.1.1",
      "signal": 100,
      "frequency_mhz": 5200
    }
  },
  "msg": "成功"
}
```

`available: false` 时会带 `reason`。

### `POST /k20mon/operations/wifi/scan`

| 参数 | 位置 | 说明 |
|---|---|---|
| `interface` | query | 可选，留空自动选 |

```bash
curl -X POST "http://127.0.0.1:18888/k20mon/operations/wifi/scan?interface=wld0" -H "x-token: $TOKEN"
```

```json
{
  "code": 0,
  "data": {
    "ok": true,
    "interface": "wld0",
    "networks": [
      {"ssid": "PDCN_5G", "in_use": true, "signal": 100, "security": "WPA2", "channel": "36", "frequency_mhz": "5200", "interface": "wld0"},
      {"ssid": "CMCC-2.4G", "in_use": false, "signal": 72, "security": "WPA2", "channel": "6", "frequency_mhz": "2437", "interface": "wld0"}
    ]
  },
  "msg": "成功"
}
```

> 这个接口要 `--rescan yes`，实测 3-8 秒，前端要加 loading。

### `POST /k20mon/operations/wifi/connect`

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `interface` | string | 否 | 留空自动选 |
| `ssid` | string | 是 | ≤ 255 字符 |
| `password` | string | 否 | ≤ 128 字符，开放网络留空 |

```bash
curl -X POST http://127.0.0.1:18888/k20mon/operations/wifi/connect \
  -H "x-token: $TOKEN" -H 'Content-Type: application/json' \
  -d '{"interface":"wld0","ssid":"PDCN_5G","password":"你的密码"}'
```

```json
{"code": 0, "data": {"ok": true, "message": "Wi-Fi 连接命令已执行"}, "msg": "成功"}
```

> 返回只代表命令执行成功，**不代表已经连上**。连上与否要再查一次 `status`。

### `POST /k20mon/operations/wifi/disconnect`

```bash
curl -X POST http://127.0.0.1:18888/k20mon/operations/wifi/disconnect \
  -H "x-token: $TOKEN" -H 'Content-Type: application/json' -d '{"interface":"wld0"}'
```

```json
{"code": 0, "data": {"ok": true, "message": "Wi-Fi 已断开"}, "msg": "成功"}
```

### `GET /k20mon/operations/cloudflare/status`

```json
{
  "code": 0,
  "data": {
    "ok": true,
    "cloudflare": {
      "ok": true,
      "running": true,
      "mode": "token",
      "config_file": "/etc/cloudflared/config.yml",
      "token_configured": true,
      "tunnel_id": "3d568d14-e6aa-4cd9-8c64-185f4f963c8c",
      "ingress": [
        {"hostname": "k20mon.example.com", "service": "http://127.0.0.1:8899"}
      ],
      "reason": null
    }
  },
  "msg": "成功"
}
```

`mode`：`token`（有 token 文件）/ `config`（只有 config.yml）/ `unknown`。

### `GET /k20mon/operations/rate-sync`

```json
{"code": 0, "data": {"ok": true, "rate_sync": {"available": true, "config": {...}, "timer": {...}, "pricing": {...}, "recent_logs": []}}, "msg": "成功"}
```

> 这个模块控制外部脚本 `sync-group-rate.sh`，不是本项目的一部分。缺文件时 `available: false`。

### `PUT /k20mon/operations/rate-sync/config`

请求体是任意 JSON 对象，原样转发给代理。字段含义见 `deploy/agent/rate_sync.py` 的 `DEFAULTS`。

### `POST /k20mon/operations/rate-sync/run`

立即执行一次，无请求体。

### `GET /k20mon/operations/frp/config`

```json
{
  "code": 0,
  "data": {
    "ok": true,
    "tunnels": {
      "ssh": {"name": "ssh", "type": "tcp", "local_ip": "127.0.0.1", "local_port": 22, "remote_port": 57923, "status": "configured", "endpoint": null},
      "mysql": {"name": "mysql", "type": "tcp", "local_ip": "127.0.0.1", "local_port": 8080, "remote_port": 31608, "status": "configured", "endpoint": null}
    }
  },
  "msg": "成功"
}
```

### `PUT /k20mon/operations/frp/config`

| 参数 | 类型 | 说明 |
|---|---|---|
| `tunnels` | array | 最多 16 个，每个 `{name, local_ip, local_port, remote_port}` |

```bash
curl -X PUT http://127.0.0.1:18888/k20mon/operations/frp/config \
  -H "x-token: $TOKEN" -H 'Content-Type: application/json' \
  -d '{"tunnels":[{"name":"ssh","local_ip":"127.0.0.1","local_port":22,"remote_port":57923}]}'
```

```json
{"code": 0, "data": {"ok": true, "message": "frp 配置已保存", "tunnels": {...}}, "msg": "成功"}
```

约束：`name` 匹配 `[A-Za-z0-9_.-]{1,32}` 且不能是 `common`；端口 1..65535；`local_ip` 匹配 `[A-Za-z0-9_.:-]{1,64}`。

> 写入前会备份成 `<原文件>.before-k20mon-<epoch>`。`[common]` 段原样保留，其它自定义键也保留。

### `POST /k20mon/operations/frp/restart`

```bash
curl -X POST http://127.0.0.1:18888/k20mon/operations/frp/restart -H "x-token: $TOKEN"
```

```json
{"code": 0, "data": {"ok": true, "message": "frpc 正在重启"}, "msg": "成功"}
```

> 执行的是 `docker restart <容器名>`，断连几秒是正常的。

---

## 服务宫格

### `GET /k20mon/operations/services/status`

从数据库读目录，交给代理并发探活。

```json
{
  "code": 0,
  "data": {
    "ok": true,
    "checked_at": 1792471643.6,
    "total": 8,
    "online": 6,
    "offline": 1,
    "services": [
      {
        "id": "1panel", "name": "1Panel", "description": "服务器管理面板",
        "category": "管理", "owner_type": "mine", "url": "https://1panel.example.com",
        "enabled": true, "state": "online", "reachable": true,
        "http_code": 200, "response_ms": 42, "reason": null, "checked_at": 1792471643.6
      }
    ]
  },
  "msg": "成功"
}
```

`state` 取值：`online`（HTTP < 500）/ `error`（HTTP ≥ 500）/ `offline`（连不上）/ `disabled`（已停用，不探测）。

> 探活是并发 `curl`，最多 8 个线程，单个 8 秒超时。8 个服务全挂时这个接口要 8 秒才返回。

### `POST /k20mon/operations/services`

| 参数 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `id` | string | 是 | `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` |
| `name` | string | 是 | ≤ 120 字符 |
| `description` | string | 否 | ≤ 240 字符 |
| `category` | string | 是 | ≤ 60 字符 |
| `ownerType` | string | 是 | `mine` / `other`（也接受 `owner_type`） |
| `url` | string | 是 | http/https |
| `enabled` | bool | 否 | 默认 true |

```bash
curl -X POST http://127.0.0.1:18888/k20mon/operations/services \
  -H "x-token: $TOKEN" -H 'Content-Type: application/json' \
  -d '{"id":"jellyfin","name":"Jellyfin","description":"媒体库","category":"媒体","ownerType":"mine","url":"http://192.168.1.2:8096","enabled":true}'
```

```json
{"code": 0, "data": {"ok": true, "message": "服务已添加", "service": {"id": "jellyfin", "name": "Jellyfin", "sort_order": 8, "enabled": true}}, "msg": "成功"}
```

校验失败：

```json
{"code": 7, "data": {}, "msg": "服务 ID 只能包含字母、数字、点、下划线和短横线"}
{"code": 7, "data": {}, "msg": "服务地址必须是 http 或 https URL"}
{"code": 7, "data": {}, "msg": "服务归属必须是 mine 或 other"}
```

### `PUT /k20mon/operations/services`

同 `POST`，必须带 `id`。保留原 `created_at` 和 `sort_order`。

### `PUT /k20mon/operations/services/owner`

| 参数 | 类型 | 说明 |
|---|---|---|
| `id` | string | 服务 ID |
| `ownerType` | string | `mine` / `other` |

```bash
curl -X PUT http://127.0.0.1:18888/k20mon/operations/services/owner \
  -H "x-token: $TOKEN" -H 'Content-Type: application/json' \
  -d '{"id":"jellyfin","ownerType":"other"}'
```

### `POST /k20mon/operations/services/order`

拖拽排序，必须传**当前全部** ID（数量不一致会报"服务列表已变化，请刷新后重试排序"）。

```bash
curl -X POST http://127.0.0.1:18888/k20mon/operations/services/order \
  -H "x-token: $TOKEN" -H 'Content-Type: application/json' \
  -d '{"ids":["k20mon","1panel","jellyfin","it-tools"]}'
```

```json
{"code": 0, "data": {"ok": true, "message": "服务顺序已保存"}, "msg": "成功"}
```

### `DELETE /k20mon/operations/services/:id`

```bash
curl -X DELETE http://127.0.0.1:18888/k20mon/operations/services/jellyfin -H "x-token: $TOKEN"
```

```json
{"code": 0, "data": {"ok": true, "message": "服务已删除", "id": "jellyfin"}, "msg": "成功"}
```

不存在时：

```json
{"code": 7, "data": {}, "msg": "record not found"}
```

---

## 错误速查

| msg | 原因 | 解决 |
|---|---|---|
| `未登录或非法访问，请登录` | 没带 `x-token` | 加请求头 |
| `登录已过期，请重新登录` | token 过期或签名不对 | 重新登录 |
| `用户名不存在或者密码错误` | 账号密码错，或 `enable != 1` | 查 `sys_users` |
| `数据库暂不可用，请稍后重试` | MySQL 还没连上 | 等重试成功，或查 config |
| `k20mon database is unavailable` | 数据库中途断开 | 同上 |
| `k20mon root control agent is unavailable: ...` | 代理没起或 socket 路径不对 | `systemctl status k20mon-agent` |
| `不支持的操作` | 动作不在代理白名单 | 检查两侧白名单是否同步 |
| `服务列表已变化，请刷新后重试排序` | 排序时目录被别人改了 | 刷新页面重试 |
| `from must be RFC3339` | 时间格式错 | 用 `2026-10-20T00:00:00Z` 这种格式 |
