"""Run both Go binaries against synthetic SQLite, with no demo or mock HTTP API."""
import argparse
import json
import os
from pathlib import Path
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.request

from smoke import Terminal


def run(api, tui, captures):
    root = Path(__file__).resolve().parents[2]
    with tempfile.TemporaryDirectory(prefix="natterwire-integration-") as tmp:
        path = Path(tmp) / "chat.db"
        with sqlite3.connect(path) as db:
            db.executescript((root / "api/testdata/messages.sql").read_text())
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        url = f"http://127.0.0.1:{port}"
        with open(Path(tmp) / "api.log", "w+") as log:
            server = subprocess.Popen([api, "--db", str(path), "--port", str(port),
                                       "--contacts", str(root / "api/testdata/contacts.json"), "--pins", ""],
                                      stdout=log, stderr=log)
            terminal = None
            try:
                for _ in range(50):
                    try:
                        with urllib.request.urlopen(url + "/chats?limit=1", timeout=1) as response:
                            page = json.load(response)
                        break
                    except OSError:
                        if server.poll() is not None:
                            log.seek(0)
                            raise AssertionError(log.read())
                        time.sleep(0.1)
                else:
                    raise AssertionError("API did not start")
                assert page["items"][0]["displayName"] == "Alex Chen"
                terminal = Terminal(tui, ["--url", url])
                terminal.expect("Archived message")
                terminal.expect("fixture.png")
                terminal.expect("Alex Chen")
                terminal.capture(captures, "go-api-transcript")
                terminal.send("J")
                terminal.expect("Saturday at ten?")
                terminal.expect("Sam Rivera")
                terminal.send("iLocal draft")
                terminal.send("\r")
                terminal.expect("Not sent:")
                # A real WAL writer's committed update must appear without reopening the API.
                with sqlite3.connect(path) as db:
                    db.execute("PRAGMA journal_mode=WAL")
                    db.execute("UPDATE message SET text='Updated through SQLite WAL' WHERE guid='group'")
                    db.commit()
                    terminal.send("\x1b")
                    terminal.expect("J/K chats")
                    terminal.send("r")
                    terminal.expect("Updated through SQLite WAL")
                terminal.close()
                terminal = None
            finally:
                try:
                    if terminal is not None:
                        terminal.close()
                finally:
                    server.terminate()
                    server.wait(timeout=6)
            assert server.returncode == 0, server.returncode
            # Graceful shutdown releases the listening port.
            with socket.socket() as sock:
                sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                sock.bind(("127.0.0.1", port))
    print("PASS: real Go API + TUI, archived text, names, attachments, chat switch, draft, WAL refresh, shutdown")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("api")
    parser.add_argument("tui")
    parser.add_argument("--captures", type=Path)
    args = parser.parse_args()
    run(os.path.abspath(args.api), os.path.abspath(args.tui), args.captures)
