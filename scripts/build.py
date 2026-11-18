#!/usr/bin/env python3
"""Build SophyGo backend and Telegram bot binaries."""

from __future__ import annotations

import subprocess

import common


def main() -> None:
    common.BIN_DIR.mkdir(parents=True, exist_ok=True)

    common.info("building backend and bot")
    for target, output in (
        ("./backend/cmd/backend", common.BACKEND_BIN),
        ("./bot/cmd/bot", common.BOT_BIN),
    ):
        result = subprocess.run(
            ["go", "build", "-o", str(output), target],
            cwd=common.ROOT,
            check=False,
        )
        if result.returncode != 0:
            common.fail(f"go build failed for {target}")

    common.info(f"backend: {common.BACKEND_BIN}")
    common.info(f"bot: {common.BOT_BIN}")


if __name__ == "__main__":
    main()
