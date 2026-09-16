#!/usr/bin/env python3
"""Apply sql/schema.sql to the configured database.

This is a placeholder for real migrations (e.g. Alembic) — fine while
there's a single table, worth revisiting once the schema needs to
evolve incrementally against data that already exists.
"""

from pathlib import Path

from mud.db import get_connection

SCHEMA_FILE = Path(__file__).parent / "sql" / "schema.sql"


def main() -> None:
    sql = SCHEMA_FILE.read_text()
    with get_connection() as conn:
        with conn.cursor() as cur:
            cur.execute(sql)
    print(f"Applied {SCHEMA_FILE}")


if __name__ == "__main__":
    main()
