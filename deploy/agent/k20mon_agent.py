#!/usr/bin/env python3
"""K20Mon root control agent.

Runs as root and exposes a *whitelisted* set of privileged operations over a
Unix domain socket. The Go backend (unprivileged) sends one line of JSON and
reads one line of JSON back:

    {"action": "wifi_scan", "data": {"interface": "wld0"}}
    {"ok": true, "interface": "wld0", "networks": [...]}

Nothing here writes secrets to disk: Wi-Fi passwords and frp configs are only
forwarded to the underlying tools. Every mutating write keeps a timestamped
backup of the file it replaces.
"""

from __future__ import annotations

import configparser
import concurrent.futures
import json
import os
import re
import shutil
import socket
import subprocess
import tempfile
import threading
import time
from pathlib import Path

from rate_sync import configure as configure_rate_sync
from rate_sync import run_now as run_rate_sync
from rate_sync import status as rate_sync_status

CONTROL_SOCKET = Path(os.environ.get(
    "K20MON_CONTROL_SOCKET", "/run/k20mon-status/control.sock"
))
CONTROL_SOCKET_MODE = int(os.environ.get("K20MON_CONTROL_SOCKET_MODE", "0o660"), 8)

FRP_CONTAINER = os.environ.get("K20MON_FRP_CONTAINER", "natfrp-frpc")
CHARGE_GUARD_CONFIG = Path(os.environ.get(
    "K20MON_CHARGE_GUARD_CONFIG", "/etc/k20-charge-guard.conf"
))
CHARGE_GUARD_STATE = Path(os.environ.get(
    "K20MON_CHARGE_GUARD_STATE", "/run/k20-charge-guard.state"
))
CHARGE_CURRENT_MAX = Path(os.environ.get(
    "K20MON_CHARGE_CURRENT_MAX", "/sys/class/power_supply/pm8150b-charger/current_max"
))
CHARGE_CAPACITY = Path(os.environ.get(
    "K20MON_CHARGE_CAPACITY", "/sys/class/power_supply/qcom-battery/capacity"
))
CLOUDFLARED_CONFIG = Path(os.environ.get(
    "K20MON_CLOUDFLARED_CONFIG", "/etc/cloudflared/config.yml"
))
CLOUDFLARED_TOKEN_FILE = Path(os.environ.get(
    "K20MON_CLOUDFLARED_TOKEN_FILE", "/etc/cloudflared/tunnel.token"
))
SERVICES_FILE = Path(os.environ.get(
    "K20MON_SERVICES_FILE", "/etc/k20mon/services.json"
))

MAX_TUNNELS = 16
MAX_REQUEST_BYTES = 128 * 1024


# --------------------------------------------------------------------------- #
# subprocess helpers
# --------------------------------------------------------------------------- #

def command(args: list[str], timeout: float = 8.0) -> tuple[int, str, str]:
    try:
        result = subprocess.run(
            args, capture_output=True, text=True, errors="replace", timeout=timeout
        )
        return result.returncode, result.stdout, result.stderr
    except (OSError, subprocess.TimeoutExpired) as exc:
        return -1, "", str(exc)


def split_nmcli(line: str) -> list[str]:
    """Split nmcli terse output while honoring its backslash escaping."""
    fields: list[str] = []
    buf: list[str] = []
    escaped = False
    for char in line.rstrip("\n"):
        if escaped:
            buf.append(char)
            escaped = False
        elif char == "\\":
            escaped = True
        elif char == ":":
            fields.append("".join(buf))
            buf = []
        else:
            buf.append(char)
    if escaped:
        buf.append("\\")
    fields.append("".join(buf))
    return fields


def nmcli(args: list[str], timeout: float = 8.0) -> tuple[int, str, str]:
    """Run nmcli when available; minimal Ubuntu installs fall back to sysfs."""
    binary = os.environ.get("K20MON_NMCLI", "").strip()
    if not binary:
        binary = shutil.which("nmcli") or "/usr/bin/nmcli"
    return command([binary, *args], timeout=timeout)


def ip_command(args: list[str], timeout: float = 8.0) -> tuple[int, str, str]:
    binary = shutil.which("ip") or "/sbin/ip"
    return command([binary, *args], timeout=timeout)


# --------------------------------------------------------------------------- #
# Wi-Fi
# --------------------------------------------------------------------------- #

def interface_ipv4(device: str) -> tuple[str | None, str | None, str | None]:
    """Return IPv4, CIDR and default gateway without depending on NetworkManager."""
    rc, out, _ = ip_command(["-4", "-o", "addr", "show", "dev", device])
    ip = None
    cidr = None
    if rc == 0:
        match = re.search(r"\binet\s+(\d+\.\d+\.\d+\.\d+/\d+)", out)
        if match:
            cidr = match.group(1)
            ip = cidr.split("/", 1)[0]
    rc, out, _ = ip_command(["route", "show", "default", "dev", device])
    gateway = None
    if rc == 0:
        match = re.search(r"\bvia\s+(\d+\.\d+\.\d+\.\d+)", out)
        if match:
            gateway = match.group(1)
    return ip, cidr, gateway


def select_wifi_device() -> str:
    configured = os.environ.get("K20MON_WIFI_DEVICE", "").strip()
    if configured:
        return configured
    rc, out, _ = nmcli([
        "-t",
        "-f", "GENERAL.DEVICE,GENERAL.TYPE,GENERAL.STATE",
        "device", "status",
    ])
    candidates: list[tuple[int, str]] = []
    if rc == 0:
        for line in out.splitlines():
            fields = split_nmcli(line)
            if len(fields) < 3 or fields[1] not in {"wifi", "802-11-wireless"}:
                continue
            state = fields[2]
            priority = 0 if state.startswith("100") else 1
            candidates.append((priority, fields[0]))
    if candidates:
        candidates.sort()
        return candidates[0][1]
    # NetworkManager may be absent while the kernel still has a usable Wi-Fi link.
    for path in sorted(Path("/sys/class/net").glob("*")):
        name = path.name
        if not name.startswith(("wl", "wlan", "wlp", "wld")):
            continue
        try:
            if (path / "operstate").read_text().strip() in {"up", "unknown"}:
                return name
        except OSError:
            continue
    return "wld0"


def wifi_status() -> dict:
    device = select_wifi_device()

    rc, out, err = nmcli([
        "-t",
        "-f", "GENERAL.DEVICE,GENERAL.STATE,GENERAL.CONNECTION,IP4.ADDRESS,IP4.GATEWAY",
        "device", "show", device,
    ])
    details: dict[str, str] = {}
    if rc == 0:
        for line in out.splitlines():
            if ":" not in line:
                continue
            key, value = line.split(":", 1)
            details[key] = value
    state = details.get("GENERAL.STATE", "")
    fallback_ip, fallback_cidr, fallback_gateway = interface_ipv4(device)
    connected = state.startswith("100") or bool(fallback_ip)
    result = {
        "available": rc == 0 or fallback_ip is not None,
        "interface": details.get("GENERAL.DEVICE", device),
        "state": "connected" if connected else (state or "unknown"),
        "connected": connected,
        "connection": details.get("GENERAL.CONNECTION") or None,
        "ssid": None,
        "ip": (details.get("IP4.ADDRESS[1]") or "").split("/", 1)[0] or fallback_ip,
        "cidr": details.get("IP4.ADDRESS[1]") or fallback_cidr,
        "gateway": details.get("IP4.GATEWAY") or fallback_gateway,
        "signal": None,
        "frequency_mhz": None,
    }
    if rc != 0 and not fallback_ip:
        result["reason"] = err.strip() or "nmcli 读取失败"
        return result

    rc, out, err = nmcli([
        "-t",
        "-f", "IN-USE,SSID,SIGNAL,CHAN,FREQ,DEVICE",
        "device", "wifi", "list", "ifname", result["interface"],
    ])
    if rc == 0:
        for line in out.splitlines():
            fields = split_nmcli(line)
            if len(fields) < 6:
                continue
            if fields[0] == "*" or fields[1] == result["connection"]:
                result["ssid"] = fields[1] or result["connection"]
                try:
                    result["signal"] = int(fields[2])
                except (ValueError, TypeError):
                    pass
                match = re.search(r"\d+(?:\.\d+)?", fields[4])
                if match:
                    result["frequency_mhz"] = float(match.group(0))
                break
    elif connected and not result["ssid"]:
        result["reason"] = err.strip() or "Wi-Fi 扫描失败"
    return result


def wifi_scan(device: str | None = None) -> dict:
    device = device or select_wifi_device()
    rc, out, err = nmcli([
        "-t",
        "-f", "IN-USE,SSID,SIGNAL,SECURITY,CHAN,FREQ,DEVICE",
        "device", "wifi", "list", "ifname", device, "--rescan", "yes",
    ], timeout=20.0)
    if rc != 0:
        return {"ok": False, "error": "Wi-Fi 扫描失败", "detail": err.strip()[:240]}
    networks = []
    seen = set()
    for line in out.splitlines():
        fields = split_nmcli(line)
        if len(fields) < 7:
            continue
        ssid = fields[1].strip()
        if not ssid:
            continue
        key = (ssid, fields[3])
        if key in seen:
            continue
        seen.add(key)
        try:
            signal = int(fields[2])
        except (TypeError, ValueError):
            signal = None
        networks.append({
            "ssid": ssid,
            "in_use": fields[0] == "*",
            "signal": signal,
            "security": fields[3] or "开放网络",
            "channel": fields[4] or None,
            "frequency_mhz": fields[5] or None,
            "interface": fields[6] or device,
        })
    networks.sort(key=lambda item: (not item["in_use"], -(item["signal"] or 0), item["ssid"]))
    return {"ok": True, "interface": device, "networks": networks}


def wifi_connect(device: str | None, ssid: str, password: str) -> dict:
    device = device or select_wifi_device()
    if not isinstance(ssid, str) or not ssid.strip() or len(ssid) > 255:
        return {"ok": False, "error": "SSID 无效"}
    if not isinstance(password, str) or len(password) > 128:
        return {"ok": False, "error": "Wi-Fi 密码长度无效"}
    args = ["device", "wifi", "connect", ssid, "ifname", device]
    if password:
        args.extend(["password", password])
    rc, _, err = nmcli(args, timeout=30.0)
    if rc != 0:
        return {"ok": False, "error": "Wi-Fi 连接失败", "detail": err.strip()[:240]}
    return {"ok": True, "message": "Wi-Fi 连接命令已执行"}


def wifi_disconnect(device: str | None) -> dict:
    device = device or select_wifi_device()
    rc, _, err = nmcli(["device", "disconnect", "ifname", device])
    if rc != 0:
        return {"ok": False, "error": "Wi-Fi 断开失败", "detail": err.strip()[:240]}
    return {"ok": True, "message": "Wi-Fi 已断开"}


# --------------------------------------------------------------------------- #
# frp tunnels
# --------------------------------------------------------------------------- #

def frp_config_path() -> str | None:
    rc, out, _ = command([
        "/usr/bin/docker", "inspect", "-f",
        "{{range .Mounts}}{{if eq .Destination \"/etc/natfrp/frpc.ini\"}}{{.Source}}{{end}}{{end}}",
        FRP_CONTAINER,
    ])
    path = out.strip()
    return path if rc == 0 and path else None


def configured_tunnels() -> dict[str, dict]:
    path = frp_config_path()
    if not path:
        return {}
    parser = configparser.RawConfigParser()
    parser.optionxform = str
    try:
        with open(path, encoding="utf-8", errors="replace") as handle:
            parser.read_file(handle)
    except OSError:
        return {}
    tunnels: dict[str, dict] = {}
    for name in parser.sections():
        if name.lower() == "common":
            continue
        section = parser[name]
        tunnel = {
            "name": name,
            "type": section.get("type", "tcp"),
            "local_ip": section.get("local_ip", ""),
            "local_port": None,
            "remote_port": None,
            "status": "configured",
            "endpoint": None,
        }
        for key in ("local_port", "remote_port"):
            try:
                tunnel[key] = int(section.get(key, ""))
            except (TypeError, ValueError):
                pass
        tunnels[name] = tunnel
    return tunnels


def valid_tunnel_name(name: str) -> bool:
    return bool(re.fullmatch(r"[A-Za-z0-9_.-]{1,32}", name)) and name.lower() != "common"


def ini_section_block(text: str, section: str) -> str:
    match = re.search(rf"(?ms)^\[{re.escape(section)}\]\s*\n.*?(?=^\[|\Z)", text)
    return match.group(0).strip() if match else ""


def write_frp_config(tunnels: list[dict]) -> dict:
    path = frp_config_path()
    if not path:
        return {"ok": False, "error": "找不到 frpc 配置文件"}
    if not isinstance(tunnels, list) or len(tunnels) > MAX_TUNNELS:
        return {"ok": False, "error": "隧道数量无效"}

    try:
        raw = Path(path).read_text(encoding="utf-8", errors="replace")
    except OSError:
        return {"ok": False, "error": "无法读取 frpc 配置文件"}
    common = ini_section_block(raw, "common")
    if not common:
        return {"ok": False, "error": "frpc 配置缺少 common 段，已停止修改"}

    rendered = [common]
    names = set()
    for item in tunnels:
        if not isinstance(item, dict):
            return {"ok": False, "error": "隧道参数格式错误"}
        name = str(item.get("name", "")).strip()
        if not valid_tunnel_name(name) or name in names:
            return {"ok": False, "error": f"隧道名称无效: {name or '<空>'}"}
        names.add(name)
        local_ip = str(item.get("local_ip", "127.0.0.1")).strip() or "127.0.0.1"
        try:
            local_port = int(item.get("local_port"))
            remote_port = int(item.get("remote_port"))
        except (TypeError, ValueError):
            return {"ok": False, "error": f"隧道端口无效: {name}"}
        if not (1 <= local_port <= 65535 and 1 <= remote_port <= 65535):
            return {"ok": False, "error": f"隧道端口范围无效: {name}"}
        if not re.fullmatch(r"[A-Za-z0-9_.:-]{1,64}", local_ip):
            return {"ok": False, "error": f"本地地址无效: {name}"}
        old = ini_section_block(raw, name)
        lines = [
            f"[{name}]",
            "type = tcp",
            f"local_ip = {local_ip}",
            f"local_port = {local_port}",
            f"remote_port = {remote_port}",
        ]
        # Keep plugin/custom options while replacing only the editable endpoint keys.
        for old_line in old.splitlines()[1:]:
            stripped = old_line.strip()
            key = stripped.split("=", 1)[0].strip().lower() if "=" in stripped else ""
            if key not in {"type", "local_ip", "local_port", "remote_port"} and stripped:
                lines.append(old_line)
        rendered.append("\n".join(lines))

    content = "\n\n".join(rendered) + "\n"
    backup = f"{path}.before-k20mon-{int(time.time())}"
    try:
        Path(backup).write_text(raw, encoding="utf-8")
        Path(path).write_text(content, encoding="utf-8")
    except OSError:
        return {"ok": False, "error": "无法写入 frpc 配置文件"}
    return {"ok": True, "message": "frp 配置已保存", "tunnels": configured_tunnels()}


def frp_restart() -> dict:
    rc, _, err = command(["/usr/bin/docker", "restart", FRP_CONTAINER], timeout=30.0)
    if rc != 0:
        return {"ok": False, "error": "frpc 重启失败", "detail": err.strip()[:240]}
    return {"ok": True, "message": "frpc 正在重启"}


# --------------------------------------------------------------------------- #
# charge guard
# --------------------------------------------------------------------------- #

def charge_defaults() -> dict:
    return {
        "enabled": True,
        "low_threshold": 80,
        "resume_threshold": 65,
        "low_current_ua": 100000,
        "normal_current_ua": 3000000,
    }


def charge_guard_config() -> dict:
    values = charge_defaults()
    try:
        text = CHARGE_GUARD_CONFIG.read_text(encoding="utf-8", errors="replace")
    except OSError:
        text = ""
    for line in text.splitlines():
        if "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip().lower()
        value = value.strip()
        if key == "enabled":
            values["enabled"] = value.lower() in {"1", "true", "yes", "on"}
        elif key in {"low_threshold", "resume_threshold", "low_current_ua", "normal_current_ua"}:
            try:
                values[key] = int(value)
            except ValueError:
                pass
    return values


def charge_guard_get() -> dict:
    config = charge_guard_config()
    try:
        capacity = int(CHARGE_CAPACITY.read_text().strip())
    except (OSError, ValueError):
        capacity = None
    try:
        mode = CHARGE_GUARD_STATE.read_text().strip()
    except OSError:
        mode = "unknown"
    try:
        current_max_ua = int(CHARGE_CURRENT_MAX.read_text().strip())
    except (OSError, ValueError):
        current_max_ua = None
    return {
        "ok": True,
        "config": config,
        "mode": mode,
        "capacity": capacity,
        "current_max_ua": current_max_ua,
    }


def charge_guard_set(data: dict) -> dict:
    old = charge_guard_config()
    try:
        enabled = bool(data.get("enabled", old["enabled"]))
        low_threshold = int(data.get("low_threshold", old["low_threshold"]))
        resume_threshold = int(data.get("resume_threshold", old["resume_threshold"]))
        low_current_ua = int(data.get("low_current_ua", old["low_current_ua"]))
        normal_current_ua = int(data.get("normal_current_ua", old["normal_current_ua"]))
    except (TypeError, ValueError):
        return {"ok": False, "error": "充电限制参数必须是数字"}
    if not (50 <= resume_threshold < low_threshold <= 100):
        return {"ok": False, "error": "阈值需满足 50 <= 恢复阈值 < 限制阈值 <= 100"}
    if not (50_000 <= low_current_ua <= 10_000_000):
        return {"ok": False, "error": "低电流需在 50mA 到 10A 之间"}
    if not (50_000 <= normal_current_ua <= 10_000_000):
        return {"ok": False, "error": "正常电流需在 50mA 到 10A 之间"}
    content = (
        f"ENABLED={'1' if enabled else '0'}\n"
        f"LOW_THRESHOLD={low_threshold}\n"
        f"RESUME_THRESHOLD={resume_threshold}\n"
        f"LOW_CURRENT_UA={low_current_ua}\n"
        f"NORMAL_CURRENT_UA={normal_current_ua}\n"
    )
    backup = CHARGE_GUARD_CONFIG.with_name(
        f"{CHARGE_GUARD_CONFIG.name}.before-k20mon-{int(time.time())}"
    )
    try:
        if CHARGE_GUARD_CONFIG.exists():
            backup.write_bytes(CHARGE_GUARD_CONFIG.read_bytes())
        CHARGE_GUARD_CONFIG.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile(
            "w", encoding="utf-8", dir=CHARGE_GUARD_CONFIG.parent,
            prefix=f".{CHARGE_GUARD_CONFIG.name}.", delete=False
        ) as handle:
            handle.write(content)
            temp_name = handle.name
        os.chmod(temp_name, 0o600)
        os.replace(temp_name, CHARGE_GUARD_CONFIG)
        # Apply the effective mode immediately so the UI and kernel limit stay
        # synchronized instead of waiting for the next poll.
        if not enabled:
            CHARGE_CURRENT_MAX.write_text(str(normal_current_ua))
            CHARGE_GUARD_STATE.write_text("off\n")
        else:
            try:
                capacity = int(CHARGE_CAPACITY.read_text().strip())
            except (OSError, ValueError):
                capacity = None
            try:
                previous_mode = CHARGE_GUARD_STATE.read_text().strip()
            except OSError:
                previous_mode = "normal"
            if capacity is not None and capacity >= low_threshold:
                requested, mode = low_current_ua, "low"
            elif capacity is not None and capacity <= resume_threshold:
                requested, mode = normal_current_ua, "normal"
            elif previous_mode == "low":
                requested, mode = low_current_ua, "low"
            else:
                requested, mode = normal_current_ua, "normal"
            CHARGE_CURRENT_MAX.write_text(str(requested))
            CHARGE_GUARD_STATE.write_text(f"{mode}\n")
    except OSError as exc:
        return {"ok": False, "error": "无法写入充电守护配置", "detail": type(exc).__name__}
    return {
        "ok": True,
        "message": "充电限制配置已保存并立即应用",
        **charge_guard_get(),
    }


# --------------------------------------------------------------------------- #
# Cloudflare Tunnel
# --------------------------------------------------------------------------- #

def cloudflare_status() -> dict:
    try:
        config_text = CLOUDFLARED_CONFIG.read_text(encoding="utf-8", errors="replace")
    except OSError:
        config_text = ""

    tunnel_id = None
    hostnames: list[dict[str, str]] = []
    current_hostname = None
    for raw_line in config_text.splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("tunnel:"):
            tunnel_id = line.split(":", 1)[1].strip().strip("'\"") or None
        elif line.startswith("- hostname:"):
            current_hostname = line.split(":", 1)[1].strip().strip("'\"")
        elif line.startswith("service:") and current_hostname:
            hostnames.append({
                "hostname": current_hostname,
                "service": line.split(":", 1)[1].strip().strip("'\"") or "",
            })
            current_hostname = None

    rc, output, error = command(["/usr/bin/ps", "-eo", "args="])
    process_args = output if rc == 0 else ""
    running = any(
        "cloudflared" in line and " tunnel " in f" {line} "
        for line in process_args.splitlines()
    )
    token_configured = False
    try:
        token_configured = (
            CLOUDFLARED_TOKEN_FILE.is_file()
            and CLOUDFLARED_TOKEN_FILE.stat().st_size > 0
        )
    except OSError:
        pass
    return {
        "ok": True,
        "running": running,
        "mode": "token" if token_configured else ("config" if config_text else "unknown"),
        "config_file": str(CLOUDFLARED_CONFIG),
        "token_configured": token_configured,
        "tunnel_id": tunnel_id,
        "ingress": hostnames,
        "reason": None if running else (error.strip()[:160] if error else "cloudflared 未运行"),
    }


# --------------------------------------------------------------------------- #
# service catalog (read + probe + mirror of the DB catalog)
# --------------------------------------------------------------------------- #

SERVICE_ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$")
SERVICE_URL_RE = re.compile(r"^https?://[^/\s]+(?:/[^ \t\r\n]*)?$", re.IGNORECASE)


def service_config(data: dict, existing: dict | None = None) -> tuple[dict | None, str | None]:
    current = existing or {}
    service_id = str(data.get("id", current.get("id", ""))).strip()
    name = str(data.get("name", current.get("name", ""))).strip()
    description = str(data.get("description", current.get("description", ""))).strip()
    category = str(data.get("category", current.get("category", "服务"))).strip() or "服务"
    owner_type = str(data.get("owner_type", current.get("owner_type", "mine"))).strip().lower()
    url = str(data.get("url", current.get("url", ""))).strip()
    enabled = data.get("enabled", current.get("enabled", True)) is not False

    if not SERVICE_ID_RE.fullmatch(service_id):
        return None, "服务 ID 只能包含字母、数字、点、下划线和短横线"
    if not name or len(name) > 120:
        return None, "服务名称不能为空且不能超过 120 个字符"
    if len(description) > 240 or len(category) > 60:
        return None, "服务描述或分类过长"
    if owner_type not in {"mine", "other"}:
        return None, "服务归属必须是 mine 或 other"
    if not SERVICE_URL_RE.match(url):
        return None, "服务地址必须是 http 或 https URL"
    return {
        "id": service_id,
        "name": name,
        "description": description,
        "category": category,
        "owner_type": owner_type,
        "url": url,
        "enabled": enabled,
    }, None


def service_catalog() -> list[dict]:
    try:
        raw = json.loads(SERVICES_FILE.read_text(encoding="utf-8"))
    except (OSError, ValueError, TypeError):
        return []
    if not isinstance(raw, list):
        return []

    services: list[dict] = []
    for item in raw:
        if not isinstance(item, dict):
            continue
        normalized, error = service_config(item)
        if normalized is not None and error is None:
            services.append(normalized)
    return services


def write_service_catalog(services: list[dict]) -> bool:
    SERVICES_FILE.parent.mkdir(parents=True, exist_ok=True)
    handle, temp_path = tempfile.mkstemp(
        prefix=".k20mon-services.", dir=str(SERVICES_FILE.parent)
    )
    try:
        with os.fdopen(handle, "w", encoding="utf-8") as stream:
            json.dump(services, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(temp_path, 0o644)
        os.replace(temp_path, SERVICES_FILE)
        return True
    except OSError:
        try:
            os.unlink(temp_path)
        except FileNotFoundError:
            pass
        return False


def probe_service(service: dict) -> dict:
    started = time.monotonic()
    rc, output, error = command([
        "/usr/bin/curl", "-L", "-sS", "-o", "/dev/null",
        "-w", "%{http_code} %{time_total}",
        "--connect-timeout", "4", "--max-time", "8",
        service["url"],
    ], timeout=10.0)
    elapsed_ms = round((time.monotonic() - started) * 1000)
    http_code = None
    response_ms = elapsed_ms
    fields = output.strip().split()
    if fields:
        try:
            http_code = int(fields[0])
        except ValueError:
            pass
    if len(fields) > 1:
        try:
            response_ms = round(float(fields[1]) * 1000)
        except ValueError:
            pass
    if rc == 0 and http_code is not None and http_code < 500:
        state, reason = "online", None
    elif http_code is not None:
        state, reason = "error", f"HTTP {http_code}"
    else:
        state, reason = "offline", error.strip()[:160] or "连接失败"
    return {
        **service,
        "state": state,
        "reachable": state == "online",
        "http_code": http_code,
        "response_ms": response_ms,
        "reason": reason,
        "checked_at": time.time(),
    }


def services_status(configured=None) -> dict:
    if isinstance(configured, list):
        services = []
        for item in configured:
            if not isinstance(item, dict):
                continue
            normalized, error = service_config(item)
            if normalized is not None and error is None:
                services.append(normalized)
    else:
        services = service_catalog()
    enabled_services = [item for item in services if item["enabled"]]
    workers = min(8, max(1, len(enabled_services)))
    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
        results = {item["id"]: item for item in pool.map(probe_service, enabled_services)}
    results.update({
        item["id"]: {
            **item,
            "state": "disabled",
            "reachable": False,
            "http_code": None,
            "response_ms": None,
            "reason": "服务已停用",
            "checked_at": time.time(),
        }
        for item in services if not item["enabled"]
    })
    results = [results[item["id"]] for item in services]
    online = sum(1 for item in results if item["reachable"])
    return {
        "ok": True,
        "checked_at": time.time(),
        "total": len(results),
        "online": online,
        "offline": sum(1 for item in results if item["enabled"] and not item["reachable"]),
        "services": results,
    }


def services_sync(data: dict) -> dict:
    raw = data.get("services")
    if not isinstance(raw, list):
        return {"ok": False, "error": "服务目录格式无效"}
    services = []
    seen = set()
    for item in raw:
        if not isinstance(item, dict):
            return {"ok": False, "error": "服务配置格式无效"}
        normalized, error = service_config(item)
        if error:
            return {"ok": False, "error": error}
        if normalized["id"] in seen:
            return {"ok": False, "error": "服务 ID 重复"}
        seen.add(normalized["id"])
        services.append(normalized)
    if not write_service_catalog(services):
        return {"ok": False, "error": "服务目录保存失败"}
    return {"ok": True, "message": "服务目录已同步", "total": len(services)}


# --------------------------------------------------------------------------- #
# dispatch + socket server
# --------------------------------------------------------------------------- #

def control_action(request: dict) -> dict:
    action = request.get("action")
    data = request.get("data") or {}
    if not isinstance(data, dict):
        return {"ok": False, "error": "操作参数必须是对象"}

    if action == "charge_guard_get":
        return charge_guard_get()
    if action == "charge_guard_set":
        return charge_guard_set(data)
    if action == "wifi_status":
        return {"ok": True, "wifi": wifi_status()}
    if action == "wifi_scan":
        return wifi_scan(data.get("interface"))
    if action == "wifi_connect":
        return wifi_connect(data.get("interface"), data.get("ssid", ""), data.get("password", ""))
    if action == "wifi_disconnect":
        return wifi_disconnect(data.get("interface"))
    if action == "cloudflare_status":
        return {"ok": True, "cloudflare": cloudflare_status()}
    if action == "frp_config":
        return {"ok": True, "tunnels": configured_tunnels()}
    if action == "frp_apply":
        return write_frp_config(data.get("tunnels", []))
    if action == "frp_restart":
        return frp_restart()
    if action == "services_status":
        return services_status(data.get("services"))
    if action == "services_sync":
        return services_sync(data)
    if action == "rate_sync_status":
        return {"ok": True, "rate_sync": rate_sync_status()}
    if action == "rate_sync_config":
        try:
            return configure_rate_sync(data)
        except (TypeError, ValueError) as exc:
            return {"ok": False, "error": str(exc)}
    if action == "rate_sync_run":
        return run_rate_sync()
    return {"ok": False, "error": "不支持的操作"}


def handle_connection(conn: socket.socket) -> None:
    with conn:
        conn.settimeout(35.0)
        raw = b""
        while b"\n" not in raw and len(raw) < MAX_REQUEST_BYTES:
            chunk = conn.recv(8192)
            if not chunk:
                break
            raw += chunk
        try:
            request = json.loads(raw.split(b"\n", 1)[0].decode("utf-8"))
            if not isinstance(request, dict):
                raise ValueError("request must be an object")
            response = control_action(request)
        except (ValueError, TypeError, UnicodeError) as exc:
            response = {"ok": False, "error": f"请求格式错误: {type(exc).__name__}"}
        try:
            conn.sendall((json.dumps(response, ensure_ascii=False) + "\n").encode("utf-8"))
        except OSError:
            pass


def control_server() -> None:
    CONTROL_SOCKET.parent.mkdir(parents=True, exist_ok=True)
    try:
        CONTROL_SOCKET.unlink()
    except FileNotFoundError:
        pass
    server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    server.bind(str(CONTROL_SOCKET))
    # The backend container runs as a different uid, so the socket must be
    # world-writable inside a directory only root and the service user reach.
    os.chmod(CONTROL_SOCKET, CONTROL_SOCKET_MODE)
    server.listen(8)
    print(f"k20mon agent listening on {CONTROL_SOCKET}", flush=True)
    while True:
        conn, _ = server.accept()
        threading.Thread(target=handle_connection, args=(conn,), daemon=True).start()


def main() -> None:
    control_server()


if __name__ == "__main__":
    main()
