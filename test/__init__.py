"""Test package bootstrap.

The project isn't installed (no setup.py/pyproject packaging), so
`import mud...` only works if src/ is on sys.path. Doing it here means
every test module gets it for free just by being part of this package,
regardless of how the tests are invoked (unittest discover, running a
single file, an IDE runner, etc.).
"""

from __future__ import annotations

import sys
from pathlib import Path

_SRC = Path(__file__).resolve().parent.parent / "src"
if str(_SRC) not in sys.path:
    sys.path.insert(0, str(_SRC))
