"""Shared helpers for SophyGo build/start/stop scripts."""

from __future__ import annotations

import os
import signal
import subprocess
import sys
import time
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

RUNTIME_DIR = Path(os.environ.get("SOPHYGO_RUNTIME_DIR", "/tmp/sophygo"))
BIN_DIR = RUNTIME_DIR / "bin"
LOG_DIR = RUNTIME_DIR / "logs"
PID_DIR = RUNTIME_DIR / "pids"

BACKEND_BIN = BIN_DIR / "backend"
BOT_BIN = BIN_DIR / "bot"
BACKEND_PID = PID_DIR / "backend.pid"
BOT_PID = PID_DIR / "bot.pid"
BACKEND_LOG = LOG_DIR / "backend.log"
BOT_LOG = LOG_DIR / "bot.log"


def info(message: str) -> None:
    print(f"[sophygo] {message}", flush=True)


def fail(message: str) -> None:
    print(f"[sophygo] error: {message}", file=sys.stderr, flush=True)
    raise SystemExit(1)


def read_env_value(path: Path, key: str) -> str:
    if not path.exists():
        return ""

    value = ""
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        line = line.strip()
        if line.startswith(f"{key}="):
            value = line.split("=", 1)[1].strip()

    return value.strip().strip('"').strip("'")


def backend_addr() -> str:
    addr = os.environ.get("BACKEND_ADDR", "").strip()
    if not addr:
        addr = read_env_value(ROOT / "backend" / ".env.backend", "BACKEND_ADDR")

    return addr or "127.0.0.1:8081"


def health_url() -> str:
    return f"http://{backend_addr()}/health"


def check_config(
    bot_env_file: Path | None = None,
    backend_env_file: Path | None = None,
) -> None:
    bot_env = bot_env_file or (ROOT / "bot" / ".env.bot")
    backend_env = backend_env_file or (ROOT / "backend" / ".env.backend")

    token = read_env_value(bot_env, "TELEGRAM_BOT_TOKEN")
    if not token:
        fail(f"TELEGRAM_BOT_TOKEN is empty in {bot_env}")
    if ":" not in token:
        info("warning: TELEGRAM_BOT_TOKEN does not look like a BotFather token")

    key = read_env_value(backend_env, "YANDEX_API_KEY")
    if not key:
        info("warning: YANDEX_API_KEY is empty; voice answers will return 503")


def process_alive(pid: int) -> bool:
    try:
        stat = Path(f"/proc/{pid}/stat").read_text(encoding="utf-8")
    except OSError:
        return False

    try:
        state = stat.rsplit(")", 1)[1].split()[0]
    except IndexError:
        return False

    return state != "Z"


def running_pid(pidfile: Path) -> int | None:
    try:
        pid = int(pidfile.read_text(encoding="utf-8").strip())
    except (OSError, ValueError):
        return None

    try:
        os.kill(pid, 0)
    except OSError:
        return None

    if not process_alive(pid):
        try:
            os.waitpid(pid, os.WNOHANG)
        except (ChildProcessError, OSError):
            pass
        return None

    return pid


def start_process(
    name: str,
    binary: Path,
    pidfile: Path,
    logfile: Path,
    env: dict[str, str] | None = None,
) -> tuple[int, bool]:
    pid = running_pid(pidfile)
    if pid is not None:
        info(f"{name} already running (pid {pid})")
        return pid, False

    if not binary.exists():
        fail(f"{binary} not found; run ./scripts/build.py first")

    LOG_DIR.mkdir(parents=True, exist_ok=True)
    PID_DIR.mkdir(parents=True, exist_ok=True)
    logfile.write_text("", encoding="utf-8")

    with logfile.open("ab") as log_file:
        process = subprocess.Popen(
            [str(binary)],
            cwd=ROOT,
            env=env,
            stdout=log_file,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )

    pidfile.write_text(str(process.pid), encoding="utf-8")
    return process.pid, True


def wait_health(timeout: float = 10.0) -> bool:
    deadline = time.monotonic() + timeout
    url = health_url()

    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=1) as response:
                if response.status == 200:
                    return True
        except Exception:
            pass

        time.sleep(0.2)

    return False


def tail(path: Path, lines: int = 20) -> str:
    if not path.exists():
        return ""

    content = path.read_text(encoding="utf-8", errors="replace").splitlines()
    return "\n".join(content[-lines:])


def stop_process(name: str, pidfile: Path) -> None:
    pid = running_pid(pidfile)
    if pid is None:
        pidfile.unlink(missing_ok=True)
        info(f"{name} is not running")
        return

    info(f"stopping {name} (pid {pid})")
    try:
        os.kill(pid, signal.SIGTERM)
    except OSError:
        pidfile.unlink(missing_ok=True)
        return

    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        if not process_alive(pid):
            break
        time.sleep(0.1)
    else:
        info(f"{name} did not stop, sending SIGKILL")
        try:
            os.kill(pid, signal.SIGKILL)
        except OSError:
            pass

    try:
        os.waitpid(pid, os.WNOHANG)
    except (ChildProcessError, OSError):
        pass

    pidfile.unlink(missing_ok=True)
