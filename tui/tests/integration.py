"""Run both Go binaries against synthetic SQLite, with no demo or mock HTTP API."""
import argparse
import base64
import io
import json
import os
from pathlib import Path
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.request

from smoke import Terminal
from PIL import Image


def run(api, tui, captures):
    root = Path(__file__).resolve().parents[2]
    with tempfile.TemporaryDirectory(prefix="natterwire-integration-") as tmp:
        path = Path(tmp) / "chat.db"
        with sqlite3.connect(path) as db:
            db.executescript((root / "api/testdata/messages.sql").read_text())
            db.execute("UPDATE attachment SET filename=?,transfer_name='fixture.heic',mime_type='image/heic'",
                       (str(root / "api/testdata/orientation-6.heic"),))
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
                with urllib.request.urlopen(url + "/messages/" + page["items"][0]["id"] + "?limit=1") as response:
                    attachment = json.load(response)["items"][0]["attachments"][0]
                assert base64.b64decode(attachment["dataBase64"]) == (root / "api/testdata/orientation-6.heic").read_bytes()
                display = Image.open(io.BytesIO(base64.b64decode(attachment["displayDataBase64"])))
                assert display.format == "JPEG" and display.size == (1536, 2048)
                with urllib.request.urlopen(url + "/messages/" + page["items"][0]["id"] + "?limit=1&media=metadata") as response:
                    metadata = json.load(response)["items"][0]["attachments"][0]
                assert "dataBase64" not in metadata and "displayDataBase64" not in metadata
                assert (metadata["width"], metadata["height"]) == display.size
                with urllib.request.urlopen(url + "/attachments/" + metadata["mediaID"] + "?version=" + metadata["version"]) as response:
                    assert response.headers["Content-Type"] == "image/jpeg"
                    assert response.read() == base64.b64decode(attachment["displayDataBase64"])
                if captures:
                    captures.mkdir(parents=True, exist_ok=True)
                    display.save(captures / "heic-display.png")
                terminal = Terminal(tui, ["--url", url])
                terminal.expect("Archived message")
                terminal.expect("fixture.heic")
                terminal.expect("Alex Chen")
                terminal.capture(captures, "go-api-transcript")
                terminal.send("j")
                terminal.expect("Saturday at ten?")
                terminal.expect("Sam Rivera")
                terminal.send("iLocal draft")
                # Never invoke AppleScript from tests on a developer Mac.
                if sys.platform == "linux":
                    terminal.send("\r")
                    terminal.expect("sending unsupported")
                    terminal.send("\r")
                    terminal.expect("sending unsupported")
                terminal.expect("Local draft")
                # A real WAL writer's committed update must appear without reopening the API.
                with sqlite3.connect(path) as db:
                    db.execute("PRAGMA journal_mode=WAL")
                    db.execute("UPDATE message SET text='Updated through SQLite WAL' WHERE guid='group'")
                    db.commit()
                    terminal.send("\x1b")
                    terminal.expect("j/k chats")
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
    print("PASS: real Go API + TUI, archived text, names, HEIC display JPEG, chat switch, draft, WAL refresh, shutdown; send failure/retry checked on Linux only")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("api")
    parser.add_argument("tui")
    parser.add_argument("--captures", type=Path)
    args = parser.parse_args()
    run(os.path.abspath(args.api), os.path.abspath(args.tui), args.captures)
