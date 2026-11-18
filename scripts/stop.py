#!/usr/bin/env python3
"""Stop the SophyGo backend and Telegram bot."""

from __future__ import annotations

import common


def main() -> None:
    common.stop_process("bot", common.BOT_PID)
    common.stop_process("backend", common.BACKEND_PID)


if __name__ == "__main__":
    main()
