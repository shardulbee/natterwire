#!/usr/bin/env python3
"""Create synthetic chat.db for the real Go API and TUI. Refuse to overwrite."""
import pathlib
import sqlite3
import sys

path = pathlib.Path(sys.argv[1])
path.touch(exist_ok=False)
try:
    with sqlite3.connect(path) as db:
        db.executescript((pathlib.Path(__file__).resolve().parent.parent / "api/testdata/messages.sql").read_text())
except BaseException:
    path.unlink()
    raise
