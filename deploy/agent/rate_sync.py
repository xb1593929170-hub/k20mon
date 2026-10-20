#!/usr/bin/env python3
"""Read and safely control the existing sub2api dynamic pricing timer."""

from __future__ import annotations

import os
import re
import shlex
import subprocess
import time
from pathlib import Path

RATE_SCRIPT = Path(os.environ.get(
    "K20MON_RATE_SYNC_SCRIPT", "/usr/local/bin/sync-group-rate.sh"
))
INSTALL_SCRIPT = Path(os.environ.get(
    "K20MON_RATE_SYNC_INSTALLER", "/usr/local/bin/install-sync-timer.sh"
))
TIMER_UNIT = os.environ.get("K20MON_RATE_SYNC_TIMER", "sync-group-rate.timer")
SERVICE_UNIT = os.environ.get("K20MON_RATE_SYNC_SERVICE", "sync-group-rate.service")
MIN_INTERVAL_SECONDS = 30
MAX_INTERVAL_SECONDS = 7 * 24 * 3600
DEFAULTS = {
    "enabled": False,
    "mode": "instant",
    "margin": 0.35,
    "buffer": 0.05,
    "probe": True,
    "cost_select": "max",
    "probe_timeout": 15,
    "lookback_hours": 24,
    "min_delta": 0.02,
    "step": 0.01,
    "account_id": None,
    "group_id": 2,
    "interval": "60s",
}


def command(args: list[str], timeout: float = 8.0,
            env: dict[str, str] | None = None) -> tuple[int, str, str]:
    try:
        result = subprocess.run(
            args, capture_output=True, text=True, errors="replace",
            timeout=timeout, env=env,
        )
        return result.returncode, result.stdout, result.stderr
    except (OSError, subprocess.TimeoutExpired) as exc:
        return -1, "", str(exc)


def _number(value: object, name: str, low: float, high: float) -> float:
    try:
        parsed = float(value)
    except (TypeError, ValueError):
        raise ValueError(f"{name} 必须是数字") from None
    if parsed < low or parsed > high:
        raise ValueError(f"{name} 必须在 {low:g} 到 {high:g} 之间")
    return parsed


def interval_seconds(value: object) -> int:
    text = str(value or "").strip().lower()
    match = re.fullmatch(r"([1-9][0-9]*)(s|min|h|d)", text)
    if not match:
        raise ValueError("执行周期必须使用 30s、60s、5min 等格式")
    amount, unit = int(match.group(1)), match.group(2)
    multiplier = {"s": 1, "min": 60, "h": 3600, "d": 86400}[unit]
    seconds = amount * multiplier
    if not MIN_INTERVAL_SECONDS <= seconds <= MAX_INTERVAL_SECONDS:
        raise ValueError("执行周期必须在 30 秒到 7 天之间")
    return seconds


def normalize_config(raw: dict | None, current: dict | None = None) -> dict:
    values = dict(DEFAULTS)
    if current:
        values.update(current)
    if raw:
        values.update(raw)
    mode = str(values.get("mode", DEFAULTS["mode"])).strip().lower()
    if mode not in {"instant", "rolling"}:
        raise ValueError("定价模式只能是 instant 或 rolling")
    enabled = values.get("enabled", False)
    if type(enabled) is not bool:
        raise ValueError("enabled 必须是布尔值")
    margin = _number(values.get("margin"), "目标毛利率", 0.01, 0.95)
    buffer = _number(values.get("buffer"), "风控余量", 0.0, 0.94)
    if buffer >= margin:
        raise ValueError("风控余量必须小于目标毛利率")
    lookback = values.get("lookback_hours")
    try:
        lookback = int(lookback)
    except (TypeError, ValueError):
        raise ValueError("回看小时数必须是整数") from None
    if not 1 <= lookback <= 720:
        raise ValueError("回看小时数必须在 1 到 720 之间")
    min_delta = _number(values.get("min_delta"), "最小变价比例", 0.0, 1.0)
    step = _number(values.get("step"), "价格步长", 0.0001, 1.0)
    probe = values.get("probe", DEFAULTS["probe"])
    if type(probe) is not bool:
        raise ValueError("probe 必须是布尔值")
    cost_select = str(values.get("cost_select", DEFAULTS["cost_select"])).strip().lower()
    if cost_select not in {"max", "min"}:
        raise ValueError("多账号取值策略只能是 max 或 min")
    try:
        probe_timeout = int(values.get("probe_timeout", DEFAULTS["probe_timeout"]))
    except (TypeError, ValueError):
        raise ValueError("探针超时必须是整数秒") from None
    if not 1 <= probe_timeout <= 120:
        raise ValueError("探针超时必须在 1 到 120 秒之间")
    account_value = values.get("account_id")
    if account_value in {None, "", 0, "0"}:
        account_id = None
    else:
        try:
            account_id = int(account_value)
        except (TypeError, ValueError):
            raise ValueError("账号 ID 必须是整数，留空表示自动发现") from None
        if not 1 <= account_id <= 1_000_000:
            raise ValueError("账号 ID 必须在 1 到 1000000 之间")
    try:
        group_id = int(values.get("group_id"))
    except (TypeError, ValueError):
        raise ValueError("分组 ID 必须是整数") from None
    if not 1 <= group_id <= 1_000_000:
        raise ValueError("分组 ID 必须在 1 到 1000000 之间")
    interval = str(values.get("interval", DEFAULTS["interval"])).strip().lower()
    interval_seconds(interval)
    return {
        "enabled": enabled,
        "mode": mode,
        "margin": round(margin, 6),
        "buffer": round(buffer, 6),
        "probe": probe,
        "cost_select": cost_select,
        "probe_timeout": probe_timeout,
        "lookback_hours": lookback,
        "min_delta": round(min_delta, 6),
        "step": round(step, 6),
        "account_id": account_id,
        "group_id": group_id,
        "interval": interval,
        "interval_seconds": interval_seconds(interval),
    }


def _parse_environment(text: str) -> dict:
    result = {}
    for token in shlex.split(text.strip()):
        if "=" not in token:
            continue
        key, value = token.split("=", 1)
        if key in {
            "ACCOUNT_ID", "GROUP_ID", "MODE", "MARGIN", "BUFFER", "PROBE",
            "COST_SELECT", "PROBE_TIMEOUT", "LOOKBACK_HOURS", "MIN_DELTA", "STEP",
        }:
            result[key] = value
    return result


def _parse_properties(text: str) -> dict:
    result = {}
    for line in text.splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            result[key.strip()] = value.strip()
    return result


def _timestamp(value: str | None) -> str | None:
    if not value or value in {"n/a", "unknown", "0"}:
        return None
    return value


def _timer_list_snapshot() -> tuple[str | None, str | None]:
    rc, output, _ = command([
        "/usr/bin/systemctl", "list-timers", TIMER_UNIT,
        "--no-pager", "--no-legend", "--all",
    ])
    line = next((item.strip() for item in output.splitlines() if item.strip()), "")
    fields = line.split()
    if rc != 0 or len(fields) < 9:
        return None, None
    # systemd's human-readable table is stable: NEXT date/time/tz, then
    # LEFT, LAST date/time/tz. Keep the original strings for display.
    next_trigger = " ".join(fields[:4])
    last_trigger = " ".join(fields[5:9])
    return next_trigger, last_trigger


def _config_from_unit() -> dict:
    rc, output, _ = command([
        "/usr/bin/systemctl", "show", SERVICE_UNIT,
        "--no-pager", "--property=Environment", "--value",
    ])
    if rc != 0:
        return dict(DEFAULTS)
    env = _parse_environment(output)
    raw = {
        "enabled": timer_state().get("active", False),
        "mode": env.get("MODE", DEFAULTS["mode"]),
        "margin": env.get("MARGIN", DEFAULTS["margin"]),
        "buffer": env.get("BUFFER", DEFAULTS["buffer"]),
        "probe": env.get("PROBE", "1") not in {"0", "false", "False"},
        "cost_select": env.get("COST_SELECT", DEFAULTS["cost_select"]),
        "probe_timeout": env.get("PROBE_TIMEOUT", DEFAULTS["probe_timeout"]),
        "lookback_hours": env.get("LOOKBACK_HOURS", DEFAULTS["lookback_hours"]),
        "min_delta": env.get("MIN_DELTA", DEFAULTS["min_delta"]),
        "step": env.get("STEP", DEFAULTS["step"]),
        "account_id": env.get("ACCOUNT_ID") or DEFAULTS["account_id"],
        "group_id": env.get("GROUP_ID", DEFAULTS["group_id"]),
    }
    rc, output, _ = command([
        "/usr/bin/systemctl", "show", TIMER_UNIT,
        "--no-pager", "--property=OnUnitActiveUSec", "--value",
    ])
    raw["interval"] = output.strip() if rc == 0 and output.strip() else DEFAULTS["interval"]
    # systemd renders 1min as 1min and 60s as 1min; both are equivalent.
    if raw["interval"].endswith("min") and raw["interval"].startswith("1"):
        raw["interval"] = "60s"
    try:
        return normalize_config(raw)
    except ValueError:
        return dict(DEFAULTS)


def timer_state() -> dict:
    rc, output, error = command([
        "/usr/bin/systemctl", "show", TIMER_UNIT, "--no-pager",
        "--property=ActiveState,SubState,Result,NextElapseUSecRealtime,"
        "LastTriggerUSec,OnUnitActiveUSec,AccuracyUSec",
    ])
    props = _parse_properties(output)
    if rc != 0:
        return {
            "active": False, "state": "missing", "sub_state": "missing",
            "result": None, "next_trigger": None, "last_trigger": None,
            "interval": None, "accuracy": None,
            "error": error.strip()[:200] or "定时器不存在",
        }
    next_trigger = _timestamp(props.get("NextElapseUSecRealtime"))
    last_trigger = _timestamp(props.get("LastTriggerUSec"))
    if not next_trigger:
        listed_next, listed_last = _timer_list_snapshot()
        next_trigger = listed_next
        last_trigger = last_trigger or listed_last
    return {
        "active": props.get("ActiveState") == "active",
        "state": props.get("ActiveState", "unknown"),
        "sub_state": props.get("SubState", "unknown"),
        "result": props.get("Result"),
        "next_trigger": next_trigger,
        "last_trigger": last_trigger,
        "interval": props.get("OnUnitActiveUSec"),
        "accuracy": props.get("AccuracyUSec"),
        "error": None,
    }


def service_state() -> dict:
    rc, output, error = command([
        "/usr/bin/systemctl", "show", SERVICE_UNIT, "--no-pager",
        "--property=ActiveState,SubState,Result,ExecMainStatus,"
        "ExecMainStartTimestamp",
    ])
    props = _parse_properties(output)
    return {
        "active": props.get("ActiveState") == "active",
        "state": props.get("ActiveState", "missing" if rc != 0 else "unknown"),
        "sub_state": props.get("SubState", "unknown"),
        "result": props.get("Result"),
        "exit_status": props.get("ExecMainStatus"),
        "last_start": _timestamp(props.get("ExecMainStartTimestamp")),
        "error": error.strip()[:200] if rc != 0 and error else None,
    }


def _parse_dry_run(text: str) -> dict:
    pricing = {
        "mode": None, "cost": None, "effective_cost": None,
        "old_price": None, "price": None, "current_margin_pct": None,
        "effective_margin_pct": None, "old_risk": None, "risk": None,
        "changed": False, "raw": text.strip()[-2000:],
    }
    first = re.search(
        r"mode=(\w+)\s+(?:当前成本=([0-9.]+)\s+.*?成本=([0-9.]+)|"
        r"(?:当前成本|计价成本(?:\([^)]*\))?)=([0-9.]+))",
        text,
    )
    if first:
        cost = first.group(2) or first.group(4)
        effective_cost = first.group(3) or cost
        pricing.update(mode=first.group(1), cost=float(cost),
                       effective_cost=float(effective_cost))
    price = re.search(
        r"售价:\s*([0-9.]+)\s*->\s*([0-9.]+).*?当前成本毛利率\s*([0-9.]+)%"
        r"\s*/\s*计费口径毛利率\s*([0-9.]+)%",
        text,
    )
    if not price:
        price = re.search(
            r"售价\s*([0-9.]+)\s*->\s*([0-9.]+).*?毛利率\s*([0-9.]+)%",
            text,
        )
    if price:
        pricing.update(
            old_price=float(price.group(1)), price=float(price.group(2)),
            current_margin_pct=float(price.group(3)),
            effective_margin_pct=float(
                price.group(4) if price.lastindex >= 4 else price.group(3)
            ),
        )
    risk = re.search(
        r"profit_min_margin\s*([0-9.]+)\s*->\s*([0-9.]+)", text
    )
    if not risk:
        risk = re.search(r"风控\s*([0-9.]+)\s*->\s*([0-9.]+)", text)
    if risk:
        pricing.update(old_risk=float(risk.group(1)), risk=float(risk.group(2)))
    pricing["changed"] = "售价已更新" in text or "风控阈值已更新" in text
    return pricing


def dry_run(config: dict) -> dict:
    if not RATE_SCRIPT.is_file():
        return {"ok": False, "error": f"找不到 {RATE_SCRIPT}"}
    env = os.environ.copy()
    env.update({
        "GROUP_ID": str(config["group_id"]),
        "MODE": config["mode"],
        "MARGIN": str(config["margin"]),
        "BUFFER": str(config["buffer"]),
        "PROBE": "1" if config["probe"] else "0",
        "COST_SELECT": config["cost_select"],
        "PROBE_TIMEOUT": str(config["probe_timeout"]),
        "LOOKBACK_HOURS": str(config["lookback_hours"]),
        "MIN_DELTA": str(config["min_delta"]),
        "STEP": str(config["step"]),
        "DRY_RUN": "1",
    })
    rc, output, error = command([str(RATE_SCRIPT)], timeout=20.0, env=env)
    parsed = _parse_dry_run(output)
    if rc != 0:
        return {"ok": False, "error": "dry-run 失败", "detail": error.strip()[-400:],
                "pricing": parsed}
    return {"ok": True, "pricing": parsed}


def recent_logs() -> list[str]:
    rc, output, error = command([
        "/usr/bin/journalctl", "-u", SERVICE_UNIT, "-n", "12",
        "--no-pager", "-o", "short-iso",
    ], timeout=8.0)
    if rc != 0:
        return [error.strip()[:200]] if error else []
    return [line[-500:] for line in output.splitlines()[-12:]]


def status() -> dict:
    timer = timer_state()
    service = service_state()
    config = _config_from_unit()
    logs = recent_logs()
    pricing = _parse_dry_run("\n".join(logs))
    return {
        "available": RATE_SCRIPT.is_file() and INSTALL_SCRIPT.is_file(),
        "script": str(RATE_SCRIPT),
        "installer": str(INSTALL_SCRIPT),
        "config": config,
        "timer": timer,
        "service": service,
        "pricing": pricing if pricing["cost"] is not None else None,
        "preview_error": None if pricing["cost"] is not None else (
            "尚无可解析的最近定价记录"
        ),
        "recent_logs": logs,
        "checked_at": time.time(),
        "scope": "sub2api group rate_multiplier",
        "database_migration_required": False,
    }


def configure(raw: dict) -> dict:
    current = _config_from_unit()
    config = normalize_config(raw, current)
    if not INSTALL_SCRIPT.is_file():
        return {"ok": False, "error": f"找不到 {INSTALL_SCRIPT}"}
    if not config["enabled"]:
        rc, output, error = command([str(INSTALL_SCRIPT), "uninstall"], timeout=45.0)
    else:
        env = os.environ.copy()
        env.update({
            "MODE": config["mode"],
            "MARGIN": str(config["margin"]),
            "BUFFER": str(config["buffer"]),
            "PROBE": "1" if config["probe"] else "0",
            "COST_SELECT": config["cost_select"],
            "PROBE_TIMEOUT": str(config["probe_timeout"]),
            "LOOKBACK_HOURS": str(config["lookback_hours"]),
            "MIN_DELTA": str(config["min_delta"]),
            "STEP": str(config["step"]),
            "GROUP_ID": str(config["group_id"]),
            "INTERVAL": config["interval"],
        })
        if config["account_id"] is not None:
            env["ACCOUNT_ID"] = str(config["account_id"])
        rc, output, error = command([str(INSTALL_SCRIPT)], timeout=60.0, env=env)
    if rc != 0:
        return {
            "ok": False, "error": "定价定时器配置失败",
            "detail": (error or output).strip()[-600:],
        }
    return {"ok": True, "message": "定价定时器配置已应用",
            "rate_sync": status()}


def run_now() -> dict:
    if not RATE_SCRIPT.is_file():
        return {"ok": False, "error": f"找不到 {RATE_SCRIPT}"}
    if not status()["config"]["enabled"]:
        return {"ok": False, "error": "定价定时器未启用"}
    rc, output, error = command([
        "/usr/bin/systemctl", "start", SERVICE_UNIT,
    ], timeout=45.0)
    if rc != 0:
        return {"ok": False, "error": "立即执行定价失败",
                "detail": (error or output).strip()[-600:]}
    return {"ok": True, "message": "定价任务已执行", "rate_sync": status()}
