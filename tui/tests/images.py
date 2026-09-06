"""Verify inline media, clipping, resize, and chat switching in the real PTY client."""
import argparse
import base64
import http.server
import json
from pathlib import Path
import threading

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
        terminal.expect("[Image preview unavailable: missing.jpg]")
        terminal.expect("[Attachment: notes.pdf]")
        assert "\ufffc" not in terminal.text()
        assert image_cells(terminal), "Preview emitted no colored cells"
        terminal.capture(captures, "images")
        terminal.send("ljjjkkk")
        terminal.resize(70, 18)
        terminal.read(0.5)
        assert image_cells(terminal)
        assert all(25 <= x < 69 and 2 <= y < 17 for x, y in image_cells(terminal)), "Preview escaped transcript"
        terminal.capture(captures, "images-clipped")
        terminal.send("u")
        terminal.send("i")
        terminal.expect("Draft")
        assert all(y < 13 for _, y in image_cells(terminal)), "Preview overwrote composer"
        terminal.capture(captures, "images-draft")
        terminal.send("\x1b")
        terminal.send("hj")
        terminal.expect("No images in this chat.")
        assert not image_cells(terminal), "Image cells leaked into another chat"
        terminal.resize(110, 32)
        terminal.send("k")
        terminal.expect("[Image: natterwire.png]")
        assert image_cells(terminal), "Cached image disappeared"
        terminal.send("r")
        terminal.read(0.5)
        assert image_cells(terminal), "Refresh removed image"
        print("PASS: inline preview, fallbacks, scrolling, clipping, resize, draft, chat switch, refresh")
    finally:
        terminal.close()
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=lambda p: str(Path(p).resolve()))
    parser.add_argument("--captures", type=Path)
    args = parser.parse_args()
    run(args.binary, args.captures)
