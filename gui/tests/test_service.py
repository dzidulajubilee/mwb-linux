"""Tests for mwb_gui.service that need no systemd or GTK."""

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from mwb_gui import service  # noqa: E402


class ExecStartTests(unittest.TestCase):
    def test_parses_systemd_exec_start(self):
        value = ("{ path=/usr/bin/mwb ; argv[]=/usr/bin/mwb -bidi -edge left ; ignore_errors=no ; "
                 "start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }")
        self.assertEqual(service._parse_exec_start(value), ["/usr/bin/mwb", "-bidi", "-edge", "left"])
        self.assertIsNone(service._parse_exec_start(""))

    def test_overriding_flags(self):
        # debian/mwb.service passes these, which beat config.toml.
        u = service.UnitState(exec_args=["/usr/bin/mwb", "-bidi", "-edge", "left"])
        self.assertEqual(u.overriding_flags(), ["-bidi", "-edge"])
        u = service.UnitState(exec_args=["/usr/local/bin/mwb", "-debug"])
        self.assertEqual(u.overriding_flags(), [])
        self.assertEqual(service.UnitState().overriding_flags(), [])

    def test_control_rejects_unknown_actions(self):
        with self.assertRaises(ValueError):
            service.control("rm -rf")


class StatusFileTests(unittest.TestCase):
    def write(self, data):
        fd, path = tempfile.mkstemp()
        with os.fdopen(fd, "w") as f:
            json.dump(data, f)
        self.addCleanup(os.unlink, path)
        return path

    def test_live_status(self):
        path = self.write({"state": "connected", "remote": "desk", "pid": os.getpid()})
        self.assertEqual(service.read_status(path)["remote"], "desk")

    def test_stale_status_from_dead_process_is_ignored(self):
        pid = os.fork()
        if pid == 0:
            os._exit(0)
        os.waitpid(pid, 0)
        path = self.write({"state": "connected", "pid": pid})
        self.assertIsNone(service.read_status(path))

    def test_missing_or_corrupt(self):
        self.assertIsNone(service.read_status("/nonexistent/status.json"))
        fd, path = tempfile.mkstemp()
        os.write(fd, b"{not json")
        os.close(fd)
        self.addCleanup(os.unlink, path)
        self.assertIsNone(service.read_status(path))
        self.assertIsNone(service.read_status(""))

    def test_status_path_needs_runtime_dir(self):
        old = os.environ.get("XDG_RUNTIME_DIR")
        try:
            os.environ["XDG_RUNTIME_DIR"] = ""
            self.assertEqual(service.status_path(), "")
            os.environ["XDG_RUNTIME_DIR"] = "/run/user/1000"
            self.assertEqual(service.status_path(), "/run/user/1000/mwb/status.json")
        finally:
            if old is None:
                os.environ.pop("XDG_RUNTIME_DIR", None)
            else:
                os.environ["XDG_RUNTIME_DIR"] = old


if __name__ == "__main__":
    unittest.main()
