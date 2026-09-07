"""Exercise the real binary in a PTY against a local, synthetic API.

Requires pyte. Optional PNG captures also require Pillow and a monospace font.
"""
import argparse
import codecs
import fcntl
import http.server
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import termios
import threading
import time
import urllib.parse

import pyte


class API(http.server.BaseHTTPRequestHandler):
    requests = []
    delay = 0
    fail = False
    newest = 60

    def log_message(self, *_):
        pass

    def do_GET(self):
        API.requests.append((time.monotonic(), self.path))
        time.sleep(API.delay)
        if API.fail:
            self.send_error(503)
            return
        url = urllib.parse.urlsplit(self.path)
        query = urllib.parse.parse_qs(url.query)
        if url.path == "/chats":
            payload = {"items": [
                {"id": "alex", "displayName": "Alex Chen", "service": "iMessage", "messageCount": API.newest},
                {"id": "weekend", "displayName": "Weekend plans", "service": "iMessage", "messageCount": 1},
                {"id": "empty", "displayName": "Empty chat", "messageCount": 0},
            ], "nextBefore": None}
        elif url.path == "/chats/alex/messages":
            end = int(query.get("before", [API.newest + 1])[0])
            start = max(1, end - 50)
            payload = {"items": [
                {"id": str(i), "text": f"Message {i:03d}: Coffee by the lake? A longer message to check wrapping and reading position.",
                 "sentAt": f"2026-09-05T14:{i // 60:02d}:{i % 60:02d}Z", "isFromMe": i % 3 == 0, "sender": "Alex"}
                for i in range(end - 1, start - 1, -1)
            ], "nextBefore": str(start) if start > 1 else None}
        elif url.path == "/chats/weekend/messages":
            payload = {"items": [{"id": "weekend-1", "text": "Bring your camera.", "isFromMe": False, "sender": "Sam"}], "nextBefore": None}
        else:
            payload = {"items": [], "nextBefore": None}
        body = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        try:
            self.wfile.write(body)
        except BrokenPipeError:
            pass


class Terminal:
    def __init__(self, binary, args, width_mode="wcwidth"):
        self.screen = pyte.Screen(110, 32)
        self.stream = pyte.Stream(self.screen)
        self.decoder = codecs.getincrementaldecoder("utf-8")("replace")
        self.recording = bytearray()
        self.width_mode = width_mode
        self.query_tail = b""
        self.render_tail = ""
        self.probe_column = 0
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.environ["TERM"] = "xterm-256color"
            # The test terminal supports color, regardless of the invoking shell.
            os.environ.pop("NO_COLOR", None)
            os.execv(binary, [binary, *args])
        self.resize(110, 32)

    def resize(self, cols, rows):
        self.screen.resize(rows, cols)
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        os.kill(self.pid, signal.SIGWINCH)

    def read(self, seconds=0.15):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            if not select.select([self.fd], [], [], min(0.05, max(0, end - time.monotonic())))[0]:
                continue
            try:
                data = os.read(self.fd, 65536)
            except OSError:
                break
            if not data:
                break
            self.recording.extend(data)
            # Parse complete CSI queries even when PTY reads split a sequence.
            queries = self.query_tail + data
            consumed = 0
            for match in re.finditer(rb"\x1b\[[0-?]*[ -/]*[@-~]", queries):
                consumed = match.end()
                seq = match.group()
                if seq in (b"\x1b[1;16H", b"\x1b[1;32H"):
                    self.probe_column = 16 if seq == b"\x1b[1;16H" else 32
                elif seq == b"\x1b[6n":
                    column = {16: 18, 32: 34}.get(self.probe_column, 1) if self.width_mode == "unicode" else {16: 17, 32: 40}.get(self.probe_column, 1)
                    os.write(self.fd, f"\x1b[1;{column}R".encode())
                    self.probe_column = 0
                elif seq == b"\x1b[5n":
                    os.write(self.fd, b"\x1b[0n")
                elif seq == b"\x1b[?2026$p":
                    os.write(self.fd, b"\x1b[?2026;2$y")
                elif seq in (b"\x1b[c", b"\x1b[0c"):
                    os.write(self.fd, b"\x1b[?1;2c")
            self.query_tail = queries[consumed:][-32:]
            # Pyte lacks colon-form SGR colors; normalize only its input, not the recording.
            rendered = self.render_tail + self.decoder.decode(data)
            partial = re.search(r"\x1b(?:\[[0-?]*[ -/]*)?$", rendered)
            self.render_tail = rendered[partial.start():] if partial else ""
            if partial:
                rendered = rendered[:partial.start()]
            rendered = re.sub(r"\x1b\[([0-9:;]*)m", lambda m: "\x1b[" + m[1].replace("::", ":").replace(":", ";") + "m", rendered)
            self.stream.feed(rendered)

    def text(self):
        return "\n".join(self.screen.display)

    def expect(self, text, seconds=3):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            self.read()
            if text in self.text():
                return
        raise AssertionError(f"Missing {text!r}:\n{self.text()}")

    def send(self, text):
        os.write(self.fd, text.encode())
        self.read()

    def capture(self, folder, name):
        if folder is None:
            return
        from PIL import Image, ImageDraw, ImageFont
        font_path = next(p for p in ["/System/Library/Fonts/Menlo.ttc", "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"] if Path(p).exists())
        font = ImageFont.truetype(font_path, 16)
        cw, ch = 10, 23
        image = Image.new("RGB", (self.screen.columns * cw + 32, self.screen.lines * ch + 32), "#151719")
        draw = ImageDraw.Draw(image)
        colors = {"default": "#d6d9df", "black": "#151719", "brightblack": "#858b96", "cyan": "#7bd5df", "yellow": "#e5c07b", "white": "#d6d9df"}
        for y in range(self.screen.lines):
            for x in range(self.screen.columns):
                cell = self.screen.buffer[y][x]
                fg = colors.get(cell.fg, "#" + cell.fg if len(cell.fg) == 6 else "#d6d9df")
                bg = "#151719" if cell.bg == "default" else colors.get(cell.bg, "#" + cell.bg if len(cell.bg) == 6 else "#151719")
                if cell.reverse:
                    fg, bg = bg, fg
                draw.rectangle((16 + x*cw, 16 + y*ch, 16 + (x+1)*cw, 16 + (y+1)*ch), fill=bg)
                draw.text((16 + x*cw, 16 + y*ch), cell.data, fill=fg, font=font)
        folder.mkdir(parents=True, exist_ok=True)
        image.save(folder / (name + ".png"))
        (folder / (name + ".ansi")).write_bytes(self.recording)

    def close(self):
        self.send("\x03")
        end = time.monotonic() + 4
        while time.monotonic() < end:
            self.read()
            pid, status = os.waitpid(self.pid, os.WNOHANG)
            if pid:
                os.close(self.fd)
                assert os.waitstatus_to_exitcode(status) == 0, self.text()
                return
        raise AssertionError("Client did not shut down promptly")


def run(binary, captures):
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), API)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    terminal = Terminal(binary, ["--url", f"http://127.0.0.1:{server.server_port}"])
    try:
        terminal.expect("Message 060")
        terminal.expect("J/K chats")
        terminal.send("J")
        terminal.expect("Bring your camera.")
        terminal.expect("J/K chats")
        assert "Message 060" not in terminal.text(), "Sidebar selection must switch the transcript"
        terminal.capture(captures, "sidebar-switch")
        # Cached switches must render even while their refresh is slow.
        API.delay = 1.5
        terminal.send("K")
        terminal.expect("Message 060", seconds=0.5)
        terminal.send("J")
        terminal.expect("Bring your camera.", seconds=0.5)
        terminal.expect("J/K chats")
        terminal.read(3.5)
        API.delay = 0
        terminal.expect("Bring your camera.")
        terminal.expect("j/k scroll")
        terminal.send("ihello jkdu")
        terminal.expect("hello jkdu")
        terminal.send("\x1b[200~ paste\njk\x1b[201~")
        terminal.expect("hello jkdu paste jk")
        terminal.send("\r")
        terminal.expect("Not sent or unconfirmed:")
        terminal.capture(captures, "draft")
        terminal.send("\x1b")
        terminal.expect("J/K chats")
        terminal.send("K")
        terminal.expect("Message 060")
        bottom = terminal.screen.display[2][32:]
        terminal.send("k")
        assert terminal.screen.display[2][32:] != bottom
        terminal.send("j")
        assert terminal.screen.display[2][32:] == bottom
        terminal.send("\x15")
        assert "Message 060" not in terminal.text()
        terminal.send("\x04")
        terminal.expect("Message 060")
        terminal.send("\x15")
        before = terminal.screen.display[2][32:]
        terminal.send("du")
        assert terminal.screen.display[2][32:] == before, "Plain d/u must not scroll"
        assert "Message 060" not in terminal.text()
        API.newest = 62
        terminal.send("r")
        terminal.expect("2 new messages")
        assert terminal.screen.display[2][32:] == before, "Refresh moved the reading position"
        terminal.capture(captures, "scrolled")
        terminal.send("G")
        terminal.expect("Message 062")
        terminal.send("o")
        terminal.read(0.8)
        assert any("before=" in path for _, path in API.requests)
        terminal.send("J")
        terminal.expect("hello jkdu")
        terminal.send("K")
        terminal.expect("Message 062")
        # A slow fetch must not block editing or switching modes.
        API.delay = 1.5
        terminal.send("riSTILL TYPING")
        terminal.expect("STILL TYPING", seconds=0.5)
        terminal.capture(captures, "loading")
        terminal.read(3.5)
        API.delay = 0
        terminal.send("\x1b")
        API.fail = True
        terminal.send("r")
        terminal.expect("Refresh failed")
        assert "Message 062" in terminal.text()
        terminal.capture(captures, "offline")
        API.fail = False
        terminal.send("r")
        terminal.read(1)
        terminal.resize(50, 10)
        terminal.expect("Resize your terminal")
        terminal.capture(captures, "small")
        terminal.resize(110, 32)
        terminal.expect("Message 062")
        # Exercise the actual 30-second timer, without a test-only polling interval.
        API.newest = 63
        terminal.expect("Message 063", seconds=32)
        terminal.capture(captures, "poll")
        # More than one page arrived since the cached head. Refresh must bridge it.
        API.newest = 180
        count = len(API.requests)
        terminal.send("r")
        terminal.expect("Message 180")
        assert sum("before=" in path for _, path in API.requests[count:]) >= 2
        print("PASS: modes, drafts, scroll anchoring, pagination, background I/O, errors, resize, 30s polling")
    finally:
        terminal.close()
        server.shutdown()
    demo = Terminal(binary, ["--demo"])
    try:
        demo.expect("Meet at the coffee shop")
        demo.capture(captures, "demo")
    finally:
        demo.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=lambda p: str(Path(p).resolve()))
    parser.add_argument("--captures", type=Path)
    args = parser.parse_args()
    run(args.binary, args.captures)
