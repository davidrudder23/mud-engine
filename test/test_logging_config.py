"""Tests for mud.logging_config.configure_logging: MUD_LOG_LEVEL parsing
and the handler it attaches to the "mud" logger."""

from __future__ import annotations

import logging
import os
import unittest
from unittest.mock import patch

from mud.logging_config import configure_logging


class ConfigureLoggingTests(unittest.TestCase):
    def setUp(self) -> None:
        logger = logging.getLogger("mud")
        self._original_level = logger.level
        self._original_handlers = list(logger.handlers)
        self._original_propagate = logger.propagate
        self.addCleanup(self._restore_logger)

    def _restore_logger(self) -> None:
        logger = logging.getLogger("mud")
        logger.handlers[:] = self._original_handlers
        logger.setLevel(self._original_level)
        logger.propagate = self._original_propagate

    def test_defaults_to_info(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            configure_logging()
        self.assertEqual(logging.getLogger("mud").level, logging.INFO)

    def test_reads_level_from_env_var(self) -> None:
        with patch.dict(os.environ, {"MUD_LOG_LEVEL": "DEBUG"}, clear=True):
            configure_logging()
        self.assertEqual(logging.getLogger("mud").level, logging.DEBUG)

    def test_env_var_is_case_insensitive(self) -> None:
        with patch.dict(os.environ, {"MUD_LOG_LEVEL": "warning"}, clear=True):
            configure_logging()
        self.assertEqual(logging.getLogger("mud").level, logging.WARNING)

    def test_unrecognized_level_falls_back_to_info(self) -> None:
        with patch.dict(os.environ, {"MUD_LOG_LEVEL": "not-a-level"}, clear=True):
            configure_logging()
        self.assertEqual(logging.getLogger("mud").level, logging.INFO)

    def test_attaches_a_handler(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            configure_logging()
        self.assertGreaterEqual(len(logging.getLogger("mud").handlers), 1)

    def test_does_not_propagate_to_root(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            configure_logging()
        self.assertFalse(logging.getLogger("mud").propagate)


if __name__ == "__main__":
    unittest.main()
