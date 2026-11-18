#!/usr/bin/env python3
"""Start the SophyGo backend and Telegram bot."""

from __future__ import annotations

import argparse
import os
import sys
import time
from pathlib import Path

import common


def resolve_path(value: str | None) -> Path | None:
    if not value:
        return None

    path = Path(value).expanduser()
    if not path.is_absolute():
        path = common.ROOT / path

    return path.resolve()


def existing_path(value: str | None, option: str) -> Path | None:
    path = resolve_path(value)
    if path is not None and not path.exists():
        common.fail(f"{option}: file not found: {path}")

    return path


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Start the SophyGo backend and Telegram bot.",
    )
    parser.add_argument(
        "--env-file",
        help="path to the bot env file with TELEGRAM_BOT_TOKEN",
    )
    parser.add_argument(
        "--settings-file",
        help="path to the bot settings JSON",
    )
    parser.add_argument(
        "--backend-env-file",
        help="path to the backend env file with YANDEX_API_KEY",
    )
    parser.add_argument(
        "--backend-settings-file",
        help="path to the backend settings JSON",
    )
    args = parser.parse_args()

    bot_env = (
        existing_path(args.env_file, "--env-file")
        or resolve_path(os.environ.get("BOT_ENV_FILE"))
        or (common.ROOT / "bot" / ".env.bot")
    )
    backend_env = (
        existing_path(args.backend_env_file, "--backend-env-file")
        or resolve_path(os.environ.get("BACKEND_ENV_FILE"))
        or (common.ROOT / "backend" / ".env.backend")
    )

    bot_settings = existing_path(args.settings_file, "--settings-file")
    backend_settings = existing_path(
        args.backend_settings_file,
        "--backend-settings-file",
    )

    common.check_config(bot_env, backend_env)

    child_env = os.environ.copy()
    child_env["BOT_ENV_FILE"] = str(bot_env)
    child_env["BACKEND_ENV_FILE"] = str(backend_env)

    if bot_settings is not None:
        child_env["BOT_SETTINGS_FILE"] = str(bot_settings)
    if backend_settings is not None:
        child_env["BACKEND_SETTINGS_FILE"] = str(backend_settings)

    backend_pid, started = common.start_process(
        "backend",
        common.BACKEND_BIN,
        common.BACKEND_PID,
        common.BACKEND_LOG,
        env=child_env,
    )

    if not common.wait_health():
        output = common.tail(common.BACKEND_LOG)
        if output:
            print(output, file=sys.stderr)
        common.stop_process("backend", common.BACKEND_PID)
        common.fail(f"backend did not become healthy at {common.health_url()}")

    if started:
        common.info(f"backend started (pid {backend_pid})")

    bot_pid, started = common.start_process(
        "bot",
        common.BOT_BIN,
        common.BOT_PID,
        common.BOT_LOG,
        env=child_env,
    )

    if started:
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            if common.running_pid(common.BOT_PID) is None:
                break
            if "bot started" in common.tail(common.BOT_LOG, 50):
                break
            time.sleep(0.2)

    if "bot started" not in common.tail(common.BOT_LOG, 50):
        output = common.tail(common.BOT_LOG)
        if output:
            print(output, file=sys.stderr)
        common.stop_process("bot", common.BOT_PID)
        if started:
            common.stop_process("backend", common.BACKEND_PID)
        common.fail("bot did not start; check TELEGRAM_BOT_TOKEN and the log above")

    if started:
        common.info(f"bot started (pid {bot_pid})")
    else:
        common.info(f"bot already running (pid {bot_pid})")


if __name__ == "__main__":
    main()
