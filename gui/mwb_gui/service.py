"""Talk to the mwb systemd user service and read the client's status file.

Everything here shells out to `systemctl --user` / `journalctl --user` with
fixed argument lists (no shell), and only for the user's own session: the
GUI never needs or asks for root.
"""

from __future__ import annotations

import json
import os
import shlex
import shutil
import subprocess
from dataclasses import dataclass

UNIT = "mwb.service"
_TIMEOUT = 10  # seconds; systemctl answers in milliseconds when healthy


@dataclass
class UnitState:
    installed: bool = False
    active: str = "unknown"      # active, inactive, failed, activating, ...
    enabled: bool = False
    exec_args: list[str] | None = None  # the ExecStart command line, if known
    error: str = ""

    @property
    def running(self) -> bool:
        return self.active in ("active", "activating", "reloading")

    def overriding_flags(self) -> list[str]:
        """Flags in ExecStart that override settings the GUI edits."""
        flags = []
        for arg in self.exec_args or []:
            name = arg.split("=", 1)[0].lstrip("-")
            if name in ("bidi", "edge", "no-clipboard", "config") and arg.startswith("-"):
                flags.append(arg)
        return flags


def _systemctl(*args: str) -> subprocess.CompletedProcess:
    exe = shutil.which("systemctl")
    if exe is None:
        raise FileNotFoundError("systemctl is not available on this system")
    return subprocess.run(
        [exe, "--user", *args],
        capture_output=True,
        text=True,
        timeout=_TIMEOUT,
        check=False,
    )


def _parse_exec_start(value: str) -> list[str] | None:
    # systemd prints: { path=/usr/bin/mwb ; argv[]=/usr/bin/mwb -bidi -edge left ; ... }
    marker = "argv[]="
    start = value.find(marker)
    if start < 0:
        return None
    end = value.find(" ;", start)
    argv = value[start + len(marker) : end if end >= 0 else None]
    try:
        return shlex.split(argv)
    except ValueError:
        return argv.split()


def query() -> UnitState:
    try:
        r = _systemctl("show", UNIT, "--property=LoadState,ActiveState,UnitFileState,ExecStart")
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError) as exc:
        return UnitState(error=str(exc))
    if r.returncode != 0:
        return UnitState(error=(r.stderr or r.stdout).strip() or "systemctl failed")
    props = {}
    for line in r.stdout.splitlines():
        k, _, v = line.partition("=")
        props[k] = v
    return UnitState(
        installed=props.get("LoadState") == "loaded",
        active=props.get("ActiveState", "unknown"),
        enabled=props.get("UnitFileState") in ("enabled", "enabled-runtime"),
        exec_args=_parse_exec_start(props.get("ExecStart", "")),
    )


def control(action: str) -> str:
    """Run start/stop/restart/enable/disable. Returns "" or an error message."""
    if action not in ("start", "stop", "restart", "enable", "disable"):
        raise ValueError(action)
    try:
        r = _systemctl(action, UNIT)
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError) as exc:
        return str(exc)
    if r.returncode != 0:
        return (r.stderr or r.stdout).strip() or f"systemctl {action} failed"
    return ""


def recent_log(lines: int = 15) -> str:
    exe = shutil.which("journalctl")
    if exe is None:
        return "journalctl is not available on this system."
    try:
        r = subprocess.run(
            [exe, "--user", "-u", UNIT, "-n", str(lines), "--no-pager", "-o", "cat"],
            capture_output=True,
            text=True,
            timeout=_TIMEOUT,
            check=False,
        )
    except (subprocess.TimeoutExpired, OSError) as exc:
        return str(exc)
    return (r.stdout or r.stderr).strip() or "No log entries yet."


# --- status file written by the client (internal/status) ---------------------

def status_path() -> str:
    run = os.environ.get("XDG_RUNTIME_DIR", "")
    if not run or not os.path.isabs(run):
        return ""
    return os.path.join(run, "mwb", "status.json")


def _pid_alive(pid: object) -> bool:
    if not isinstance(pid, int) or pid <= 0:
        return False
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True  # exists, owned by someone else
    return True


def read_status(path: str | None = None) -> dict | None:
    """Return the client's status, or None if it is not running or unknown.

    A status file left behind by a crashed client is ignored, by checking
    that its recorded PID still exists.
    """
    path = status_path() if path is None else path
    if not path:
        return None
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict) or not _pid_alive(data.get("pid")):
        return None
    return data
