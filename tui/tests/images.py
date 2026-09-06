"""Verify inline media, clipping, resize, and chat switching in the real PTY client."""
import argparse
import base64
import http.server
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
import time

from smoke import Terminal


class API(http.server.BaseHTTPRequestHandler):
    image = base64.b64encode((Path(__file__).resolve().parents[2] / "macos/Natterwire/Assets/NatterwireIcon.png").read_bytes()).decode()

    def log_message(self, *_):
        pass

    def do_GET(self):
        if self.path.startswith("/chats?"):
            items = [{"id": "images", "displayName": "Image previews"}, {"id": "text", "displayName": "Text only"}]
        elif self.path.startswith("/chats/images/"):
            items = [
                {"id": "3", "sender": "Alex", "sentAt": "2026-09-06T11:00:00Z", "text": "Here is the app icon.\ufffc", "attachments": [
                    {"id": "png", "filename": "natterwire.png", "mimeType": "image/png", "dataBase64": self.image},
                    {"id": "missing", "filename": "missing.jpg", "mimeType": "image/jpeg"},
                    {"id": "pdf", "filename": "notes.pdf", "mimeType": "application/pdf"},
                ]},
                {"id": "2", "isFromMe": True, "text": "Can you send the icon?"},
                {"id": "1", "sender": "Alex", "text": "\ufffc", "attachments": [
                    {"id": "older", "filename": "earlier.png", "mimeType": "image/png", "dataBase64": self.image},
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
        process = subprocess.Popen([
            "kitty", "--config", "NONE", "--listen-on", sock,
            "-o", "allow_remote_control=yes", "-o", "confirm_os_window_close=0",
            "-o", "initial_window_width=1100", "-o", "initial_window_height=750",
            "-o", "remember_window_size=no", "-o", "font_size=14",
            binary, "--url", f"http://127.0.0.1:{server.server_port}",
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

        try:
            screen = expect("[Attachment: notes.pdf]")
            assert "▀" not in screen and "preview unavailable" not in screen
            capture("kitty-images")
            remote("send-text", "\x15")
            capture("kitty-scrolled")
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
            remote("send-text", "q")
            assert process.wait(timeout=5) == 0
            log.seek(0)
            assert "DATA RACE" not in log.read()
            print("PASS: native Kitty images, crop, draft, chat switch, cached reopen, resize, refresh, clean exit")
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
