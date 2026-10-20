import unittest
import importlib.util
from pathlib import Path
from unittest.mock import patch

import rate_sync


class RateSyncTest(unittest.TestCase):
    def test_interval_bounds_and_units(self):
        self.assertEqual(rate_sync.interval_seconds("30s"), 30)
        self.assertEqual(rate_sync.interval_seconds("60s"), 60)
        self.assertEqual(rate_sync.interval_seconds("1min"), 60)
        with self.assertRaises(ValueError):
            rate_sync.interval_seconds("29s")
        with self.assertRaises(ValueError):
            rate_sync.interval_seconds("10")

    def test_normalize_rejects_unsafe_margin_and_preserves_config(self):
        config = rate_sync.normalize_config({
            "enabled": True,
            "mode": "instant",
            "margin": 0.35,
            "buffer": 0.05,
            "interval": "30s",
        })
        self.assertEqual(config["interval_seconds"], 30)
        self.assertIsNone(config["account_id"])
        self.assertTrue(config["probe"])
        self.assertEqual(config["cost_select"], "max")
        with self.assertRaises(ValueError):
            rate_sync.normalize_config({"margin": 0.30, "buffer": 0.30})
        with self.assertRaises(ValueError):
            rate_sync.normalize_config({"mode": "shell; reboot"})
        with self.assertRaises(ValueError):
            rate_sync.normalize_config({"cost_select": "shell; reboot"})

    def test_parse_pricing_output(self):
        output = """[2026-10-15 22:30:00] mode=instant  当前成本=0.0900  当前成本=0.0900
  售价: 0.1300 -> 0.1400   (当前成本毛利率 35.7% / 计费口径毛利率 35.7%)
  风控: profit_min_margin 0.3000 -> 0.3000
"""
        parsed = rate_sync._parse_dry_run(output)
        self.assertEqual(parsed["mode"], "instant")
        self.assertEqual(parsed["price"], 0.14)
        self.assertEqual(parsed["current_margin_pct"], 35.7)
        self.assertEqual(parsed["risk"], 0.3)

    def test_parse_current_probe_log_format(self):
        output = """  账号: 1=0.09   计价成本(max)=0.09   probe=1
[2026-10-16 01:43:41] mode=instant 当前成本=0.09  售价 0.1400 -> 0.1500  (毛利率 40.0%)  风控 0.3000 -> 0.3500
"""
        parsed = rate_sync._parse_dry_run(output)
        self.assertEqual(parsed["cost"], 0.09)
        self.assertEqual(parsed["price"], 0.15)
        self.assertEqual(parsed["current_margin_pct"], 40.0)
        self.assertEqual(parsed["risk"], 0.35)

    def test_status_reads_recent_logs_without_running_pricing_script(self):
        with patch.object(rate_sync, "RATE_SCRIPT") as script, \
                patch.object(rate_sync, "INSTALL_SCRIPT") as installer, \
                patch.object(rate_sync, "timer_state", return_value={
                    "active": True, "state": "active", "sub_state": "waiting",
                }), \
                patch.object(rate_sync, "service_state", return_value={
                    "active": False, "result": "success",
                }), \
                patch.object(rate_sync, "_config_from_unit", return_value={
                    **rate_sync.DEFAULTS, "enabled": True, "interval_seconds": 60,
                }), \
                patch.object(rate_sync, "recent_logs", return_value=[
                    "mode=instant 当前成本=0.0900 当前成本=0.0900",
                    "售价: 0.1300 -> 0.1400 (当前成本毛利率 35.7% / 计费口径毛利率 35.7%)",
                    "风控: profit_min_margin 0.3000 -> 0.3000",
                ]), \
                patch.object(rate_sync, "dry_run") as dry_run:
            script.is_file.return_value = True
            installer.is_file.return_value = True
            result = rate_sync.status()
        dry_run.assert_not_called()
        self.assertEqual(result["pricing"]["price"], 0.14)
        self.assertFalse(result["database_migration_required"])

    def test_timer_state_falls_back_to_list_timers_for_next_trigger(self):
        with patch.object(rate_sync, "command", side_effect=[
            (0, "\n".join([
                "ActiveState=active",
                "SubState=waiting",
                "Result=success",
                "NextElapseUSecRealtime=",
                "LastTriggerUSec=",
                "OnUnitActiveUSec=",
                "AccuracyUSec=5s",
            ]), ""),
            (0, "Fri 2026-10-16 02:14:13 CST 41s Fri 2026-10-16 02:13:13 CST 18s ago pricing-sync.timer pricing-sync.service\n", ""),
        ]):
            result = rate_sync.timer_state()
        self.assertEqual(result["next_trigger"], "Fri 2026-10-16 02:14:13 CST")
        self.assertEqual(result["last_trigger"], "Fri 2026-10-16 02:13:13 CST")

    def test_control_actions_expose_rate_sync_and_reject_non_object_config(self):
        module_path = Path(__file__).with_name("k20mon_agent.py")
        spec = importlib.util.spec_from_file_location("k20mon_agent", module_path)
        agent = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(agent)

        with patch.object(agent, "rate_sync_status", return_value={"available": True}):
            self.assertEqual(
                agent.control_action({"action": "rate_sync_status"}),
                {"ok": True, "rate_sync": {"available": True}},
            )
        self.assertEqual(
            agent.control_action({
                "action": "rate_sync_config",
                "data": "not-an-object",
            }),
            {"ok": False, "error": "操作参数必须是对象"},
        )
        self.assertEqual(
            agent.control_action({"action": "does_not_exist"}),
            {"ok": False, "error": "不支持的操作"},
        )


if __name__ == "__main__":
    unittest.main()
