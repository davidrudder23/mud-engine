"""Logging setup for the MUD server.

Every module logs via `logging.getLogger(__name__)`, which for this
package always starts with "mud." -- so they all nest under one "mud"
logger. `configure_logging()` attaches a single handler there and sets
its level; call it once, from server.py's main() (or an equivalent
real-process entrypoint) -- library code and tests should never call
it, the same way they shouldn't call logging.basicConfig(). Tests that
want to assert on a log record use unittest's assertLogs, which works
independently of whether configure_logging() was ever called.

Level is controlled by MUD_LOG_LEVEL (default INFO): DEBUG, INFO,
WARNING, ERROR, or CRITICAL (case-insensitive); an unrecognized value
falls back to INFO rather than raising, since a typo'd env var
shouldn't stop the server from starting.
"""

from __future__ import annotations

import logging
import os

_FORMAT = "%(asctime)s %(levelname)-8s %(name)s: %(message)s"


def configure_logging() -> None:
    level_name = os.environ.get("MUD_LOG_LEVEL", "INFO").strip().upper()
    level = getattr(logging, level_name, None)
    if not isinstance(level, int):
        level = logging.INFO

    handler = logging.StreamHandler()
    handler.setFormatter(logging.Formatter(_FORMAT))

    logger = logging.getLogger("mud")
    logger.setLevel(level)
    logger.addHandler(handler)
    # This is the root of every mud.* logger's propagation chain -- stop
    # here rather than also handing records up to the root logger, in
    # case something else (a test runner, a library) has its own
    # handlers there too.
    logger.propagate = False
