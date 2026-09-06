"""Record incremental scrolling and a clean repaint of the same emoji transcript."""
import argparse
import http.server
import json
from pathlib import Path
import threading
from smoke import Terminal


class API(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_GET(self):
        if self.path.startswith('/chats?'):
            value = {"items": [{"id": "emoji", "displayName": "Emoji test", "messageCount": 80}]}
        else:
            emojis = ["❤️", "👩‍👩‍👧‍👦", "👋🏿", "🇨🇦", "☕️", "❤"]
            value = {"items": [
                {"id": str(i), "text": f"Message {i:03d}: Unicode, wrapping, and scrolling {emojis[i % len(emojis)]}", "isFromMe": i % 2 == 0, "sender": "Alex"}
                for i in range(80, 0, -1)
            ], "nextBefore": None}
        data = json.dumps(value).encode()
        self.send_response(200)
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('binary', type=lambda p: str(Path(p).resolve()))
    parser.add_argument('captures', type=Path)
    parser.add_argument('--width', choices=['unicode', 'wcwidth'], default='unicode')
    args = parser.parse_args()
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), API)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    terminal = Terminal(args.binary, ['--url', f'http://127.0.0.1:{server.server_port}'], width_mode=args.width)
    try:
        terminal.expect('Message 080')
        for key in 'k' * 30 + 'j' * 10:
            terminal.send(key)
        terminal.capture(args.captures, 'incremental')
        terminal.resize(110, 32)
        terminal.read(0.5)
        terminal.capture(args.captures, 'repaint')
    finally:
        terminal.close()
        server.shutdown()
