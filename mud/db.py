"""Database connection handling for player persistence.

Config comes from environment variables, so the same code works across
environments without changes:

    MUD_DB_HOST      unset by default -> connect over the local Unix
                      socket using peer auth (matches your OS user)
    MUD_DB_PORT      default 5432
    MUD_DB_NAME      default "mud"
    MUD_DB_USER      unset by default -> your OS user (peer auth)
    MUD_DB_PASSWORD  unset by default

A small connection pool is used rather than one connection per call (or
one long-lived global connection): a multiplayer server needs many
concurrent DB operations in flight, and pooling is the cheapest thing
that scales towards that without adding real complexity now.
"""

from __future__ import annotations

import os
from contextlib import contextmanager

import psycopg2
import psycopg2.pool

_pool: psycopg2.pool.ThreadedConnectionPool | None = None


def _config() -> dict:
    config: dict = {"dbname": os.environ.get("MUD_DB_NAME", "mud")}
    # Only set host/port/user/password if given explicitly, so the default
    # stays a local Unix-socket connection using peer auth (no password).
    for key, env_var in [
        ("host", "MUD_DB_HOST"),
        ("port", "MUD_DB_PORT"),
        ("user", "MUD_DB_USER"),
        ("password", "MUD_DB_PASSWORD"),
    ]:
        value = os.environ.get(env_var)
        if value:
            config[key] = value
    return config


def get_pool() -> psycopg2.pool.ThreadedConnectionPool:
    global _pool
    if _pool is None:
        _pool = psycopg2.pool.ThreadedConnectionPool(1, 10, **_config())
    return _pool


@contextmanager
def get_connection():
    """Borrow a pooled connection. Commits on success, rolls back on error."""
    pool = get_pool()
    conn = pool.getconn()
    try:
        yield conn
        conn.commit()
    except Exception:
        conn.rollback()
        raise
    finally:
        pool.putconn(conn)
