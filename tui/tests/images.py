"""Verify inline media, clipping, resize, and chat switching in the real PTY client."""
import argparse
import http.server
import json
import os
from pathlib import Path
import re
import shlex
import statistics
import subprocess
import tempfile
import threading
import time

from smoke import Terminal


class API(http.server.BaseHTTPRequestHandler):
    image = (Path(__file__).resolve().parents[2] / "macos/Natterwire/Assets/NatterwireIcon.png").read_bytes()
    media_requests = 0

    def log_message(self, *_):
        pass

    def do_GET(self):
        if self.path.startswith("/attachments/"):
            API.media_requests += 1
            self.send_response(200)
            self.send_header("Content-Type", "image/png")
            self.send_header("Content-Length", str(len(self.image)))
            self.end_headers()
            self.wfile.write(self.image)
            return
        if self.path.startswith("/chats?"):
            items = [{"id": "images", "displayName": "Image previews"}, {"id": "text", "displayName": "Text only"}]
        elif self.path.startswith("/chats/images/"):
            items = [
                {"id": "3", "sender": "Alex", "sentAt": "2026-09-06T11:00:00Z", "text": "Here is the app icon.\ufffc", "attachments": [
                    {"id": "png", "filename": "natterwire.png", "mimeType": "image/png", "mediaID": "icon", "version": "1", "width": 1254, "height": 1254},
                    {"id": "missing", "filename": "missing.jpg", "mimeType": "image/jpeg"},
                    {"id": "pdf", "filename": "notes.pdf", "mimeType": "application/pdf"},
                ]},
                {"id": "2", "isFromMe": True, "text": "Can you send the icon?"},
                {"id": "1", "sender": "Alex", "text": "\ufffc", "attachments": [
                    {"id": "older", "filename": "earlier.png", "mimeType": "image/png", "mediaID": "icon", "version": "1", "width": 1254, "height": 1254},
                ]},
            ]
        else:
            items = [{"id": "text", "text": "No images in this chat."}]
        body = json.dumps({"items": items, "nextBefore": None}).encode()
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def image_cells(terminal):
    return [(x, y) for y in range(terminal.screen.lines) for x in range(terminal.screen.columns)
            if any(len(c) == 6 and all(ch in "0123456789abcdef" for ch in c)
                   for c in (terminal.screen.buffer[y][x].fg, terminal.screen.buffer[y][x].bg))]


def run(binary, captures):
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), API)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    terminal = Terminal(binary, ["--url", f"http://127.0.0.1:{server.server_port}"])
    try:
        terminal.expect("[Image: natterwire.png]")
        terminal.expect("[Image: missing.jpg]")
        terminal.expect("[Attachment: notes.pdf]")
        assert "\ufffc" not in terminal.text()
        assert not image_cells(terminal), "Non-Kitty terminal must show filenames only"
        terminal.capture(captures, "images")
        terminal.send("jjjkkk")
        terminal.resize(70, 18)
        terminal.read(0.5)
        assert not image_cells(terminal)
        terminal.capture(captures, "images-clipped")
        terminal.send("\x15")
        terminal.send("i")
        terminal.expect("Draft")
        assert all(y < 13 for _, y in image_cells(terminal)), "Preview overwrote composer"
        terminal.capture(captures, "images-draft")
        terminal.send("\x1b")
        terminal.send("J")
        terminal.expect("No images in this chat.")
        assert not image_cells(terminal), "Image cells leaked into another chat"
        terminal.resize(110, 32)
        terminal.send("K")
        terminal.expect("[Image: natterwire.png]")
        assert not image_cells(terminal), "Cached image used a non-Kitty renderer"
        terminal.send("r")
        terminal.read(0.5)
        assert not image_cells(terminal), "Refresh used a non-Kitty renderer"
        assert API.media_requests == 0, "Filename-only terminals must not fetch image bytes"
        print("PASS: filename-only fallback, scrolling, resize, draft, chat switch, refresh")
    finally:
        terminal.close()
        server.shutdown()
        server.server_close()


def run_kitty(binary, captures):
    """Run under xvfb-run. Capture the real Kitty renderer, not a block emulator."""
    from PIL import Image
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), API)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    with tempfile.TemporaryDirectory(prefix="natterwire-kitty-") as tmp:
        sock = "unix:" + tmp + "/kitty.sock"
        env = dict(os.environ, LIBGL_ALWAYS_SOFTWARE="1", LD_LIBRARY_PATH="/usr/lib/x86_64-linux-gnu")
        env.pop("NO_COLOR", None)
        log = open(Path(tmp) / "kitty.log", "w+")
        transcript = Path(tmp) / "terminal-output"
        inputs, timing = Path(tmp) / "terminal-input", Path(tmp) / "terminal-timing"
        process = subprocess.Popen([
            "kitty", "--config", "NONE", "--listen-on", sock,
            "-o", "allow_remote_control=yes", "-o", "confirm_os_window_close=0",
            "-o", "initial_window_width=1100", "-o", "initial_window_height=750",
            "-o", "remember_window_size=no", "-o", "font_size=14",
            "script", "-q", "-f", "-e", "--log-in", str(inputs), "--log-out", str(transcript), "--log-timing", str(timing), "-c",
            shlex.join([binary, "--url", f"http://127.0.0.1:{server.server_port}"]),
        ], env=env, stdout=log, stderr=log)

        def remote(*args):
            return subprocess.check_output(["kitty", "@", "--to", sock, *args], env=env, stderr=subprocess.DEVNULL).decode()

        def expect(text):
            screen = ""
            for _ in range(100):
                try:
                    screen = remote("get-text")
                    if text in screen:
                        return screen
                except subprocess.CalledProcessError:
                    pass
                time.sleep(0.1)
            log.seek(0)
            raise AssertionError(f"Kitty missing {text!r}:\n{screen}\n{log.read()}")

        def capture(name, has_image=True):
            path = Path(tmp) / (name + ".png")
            for _ in range(10):
                time.sleep(0.5)
                subprocess.run(["import", "-window", "root", str(path)], check=True)
                img = Image.open(path).convert("RGB")
                orange = [(x, y) for y in range(img.height) for x in range(img.width)
                          if (lambda c: c[0] > 170 and 65 < c[1] < 180 and c[2] < 100)(img.getpixel((x, y)))]
                if bool(orange) == has_image:
                    break
            if captures:
                captures.mkdir(parents=True, exist_ok=True)
                img.save(captures / (name + ".png"))
            assert bool(orange) == has_image, f"Unexpected Kitty image presence in {name}"
            if orange:
                assert min(x for x, _ in orange) > 300, "Image overwrote sidebar"
                assert min(y for _, y in orange) > 35, "Image overwrote header"
                if name == "kitty-draft":
                    # Sample the draft label's left edge, not cyan in the artwork.
                    cyan = {y for y in range(img.height) for x in range(340, 390)
                            if (lambda c: c[0] < 80 and c[1] > 150 and c[2] > 150)(img.getpixel((x, y)))}
                    # The last two cyan runs are Draft and the input border;
                    # earlier runs can be the sender label "You".
                    starts = sorted(y for y in cyan if y - 1 not in cyan)
                    assert len(starts) >= 2 and max(y for _, y in orange) < starts[-2], "Image overlaps draft label/composer"

        try:
            screen = expect("[Attachment: notes.pdf]")
            assert "▀" not in screen and "preview unavailable" not in screen
            capture("kitty-images")
            before_scroll = transcript.stat().st_size
            remote("send-text", "\x15")
            capture("kitty-scrolled")
            scroll_output = transcript.read_bytes()[before_scroll:]
            assert b"\x1b_Ga=p," in scroll_output, "Scroll must update image placements"
            assert b"\x1b_Gf=100" not in scroll_output, "Scroll reuploaded image pixels"
            assert b"\x1b_Ga=d,d=I," not in scroll_output, "Scroll destroyed cached image data"
            assert re.search(rb"a=p,[^;]*x=\d+,y=\d+,w=\d+,h=\d+", scroll_output), "Missing native crop command"
            for i in range(60):
                remote("send-text", "k" if i % 2 == 0 else "j")
            remote("send-text", "i")
            expect("Draft")
            capture("kitty-draft")
            remote("send-text", "\x1b")
            time.sleep(0.2)
            remote("send-text", "J")
            expect("No images in this chat.")
            capture("kitty-text-only", False)
            remote("send-text", "K")
            expect("[Attachment: notes.pdf]")
            capture("kitty-reopened")
            window = subprocess.check_output(["xdotool", "search", "--pid", str(process.pid)]).decode().splitlines()[-1]
            subprocess.run(["xdotool", "windowsize", window, "900", "600"], check=True)
            time.sleep(0.5)
            capture("kitty-resized")
            remote("send-text", "r")
            time.sleep(0.5)
            capture("kitty-refreshed")
            assert API.media_requests == 1, "Scroll, resize, refresh, and chat switch must reuse the decoded source"
            remote("send-text", "q")
            assert process.wait(timeout=5) == 0
            # util-linux script timestamps input arrival and frame output on the
            # same PTY. Excludes remote-command startup and terminal presentation.
            streams = {"I": inputs.read_bytes().split(b"\n", 1)[1], "O": transcript.read_bytes().split(b"\n", 1)[1]}
            offsets, samples, elapsed, started, tail = {"I": 0, "O": 0}, [], 0, None, b""
            for record in timing.read_text().splitlines():
                kind, delay, rest = record.split(" ", 2)
                elapsed += float(delay)
                if kind not in streams:
                    continue
                count = int(rest)
                chunk = streams[kind][offsets[kind]:offsets[kind] + count]
                offsets[kind] += count
                if kind == "I" and chunk in (b"j", b"k"):
                    assert started is None, "Scroll inputs overlapped before a frame completed"
                    started = elapsed
                elif kind == "O":
                    if started is not None and b"\x1b[?2026l" in tail + chunk:
                        samples.append((elapsed - started) * 1000)
                        started = None
                    tail = (tail + chunk)[-7:]
            assert len(samples) == 60, f"Only measured {len(samples)} scroll frames"
            print("Kitty warm image scroll, input-to-frame output: median %.2fms, p95 %.2fms, max %.2fms" % (
                statistics.median(samples), sorted(samples)[56], max(samples)))
            log.seek(0)
            assert "DATA RACE" not in log.read()
            print("PASS: native Kitty images, scroll uses placements only (zero uploads/deletes), draft, cached reopen, resize, refresh, one media fetch, clean exit")
        finally:
            if process.poll() is None:
                process.terminate()
                process.wait(timeout=5)
            log.close()
            server.shutdown()
            server.server_close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=lambda p: str(Path(p).resolve()))
    parser.add_argument("--captures", type=Path)
    parser.add_argument("--kitty", action="store_true")
    args = parser.parse_args()
    (run_kitty if args.kitty else run)(args.binary, args.captures)
