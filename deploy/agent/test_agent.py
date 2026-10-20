"""Extra coverage for the agent's pure helpers (no root, no hardware needed)."""

import importlib.util
import unittest
from pathlib import Path
from unittest.mock import patch

MODULE_PATH = Path(__file__).with_name("k20mon_agent.py")
spec = importlib.util.spec_from_file_location("k20mon_agent", MODULE_PATH)
agent = importlib.util.module_from_spec(spec)
spec.loader.exec_module(agent)


class HelperTest(unittest.TestCase):
    def test_split_nmcli_honours_escaped_colons(self):
        self.assertEqual(
            agent.split_nmcli(r"wld0:wifi:connected:My\:Net:192.0.2.5/24"),
            ["wld0", "wifi", "connected", "My:Net", "192.0.2.5/24"],
        )

    def test_split_nmcli_keeps_trailing_backslash(self):
        self.assertEqual(agent.split_nmcli(r"a:b\\"), ["a", "b\\"])

    def test_valid_tunnel_name(self):
        self.assertTrue(agent.valid_tunnel_name("ssh"))
        self.assertTrue(agent.valid_tunnel_name("web-8899"))
        self.assertFalse(agent.valid_tunnel_name("common"))
        self.assertFalse(agent.valid_tunnel_name("bad name"))
        self.assertFalse(agent.valid_tunnel_name(""))

    def test_ini_section_block_stops_at_next_section(self):
        text = "[common]\nserver = x\n\n[ssh]\ntype = tcp\n\n[web]\ntype = tcp\n"
        self.assertEqual(agent.ini_section_block(text, "ssh"), "[ssh]\ntype = tcp")

    def test_service_config_rejects_bad_values(self):
        self.assertEqual(agent.service_config({"id": "x"})[1], "服务名称不能为空且不能超过 120 个字符")
        self.assertEqual(
            agent.service_config({"id": "x", "name": "n", "url": "ftp://x"})[1],
            "服务地址必须是 http 或 https URL",
        )
        self.assertEqual(
            agent.service_config({"id": "bad id", "name": "n", "url": "https://x"})[1],
            "服务 ID 只能包含字母、数字、点、下划线和短横线",
        )
        self.assertEqual(
            agent.service_config({"id": "x", "name": "n", "url": "https://x", "owner_type": "us"})[1],
            "服务归属必须是 mine 或 other",
        )

    def test_service_config_normalizes(self):
        service, error = agent.service_config({
            "id": "web", "name": " Web ", "url": "https://example.com/a",
            "owner_type": "OTHER", "enabled": False,
        })
        self.assertIsNone(error)
        self.assertEqual(service["name"], "Web")
        self.assertEqual(service["owner_type"], "other")
        self.assertFalse(service["enabled"])

    def test_charge_guard_rejects_inverted_thresholds(self):
        result = agent.charge_guard_set({
            "low_threshold": 60, "resume_threshold": 70,
            "low_current_ua": 100000, "normal_current_ua": 3000000,
        })
        self.assertFalse(result["ok"])
        self.assertIn("阈值", result["error"])

    def test_charge_guard_rejects_out_of_range_current(self):
        result = agent.charge_guard_set({
            "low_threshold": 80, "resume_threshold": 65,
            "low_current_ua": 100, "normal_current_ua": 3000000,
        })
        self.assertFalse(result["ok"])
        self.assertIn("低电流", result["error"])

    def test_write_frp_config_rejects_bad_tunnels(self):
        with patch.object(agent, "frp_config_path", return_value=None):
            self.assertFalse(agent.write_frp_config([{"name": "ssh"}])["ok"])

    def test_control_action_rejects_non_object_data(self):
        self.assertEqual(
            agent.control_action({"action": "wifi_scan", "data": "nope"}),
            {"ok": False, "error": "操作参数必须是对象"},
        )

    def test_control_action_unknown_action(self):
        self.assertEqual(
            agent.control_action({"action": "reboot"}),
            {"ok": False, "error": "不支持的操作"},
        )


if __name__ == "__main__":
    unittest.main()
