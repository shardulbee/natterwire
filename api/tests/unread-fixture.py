"""Feed synthetic incoming messages every 30s. Never opens a real Messages DB.

Usage: python3 api/tests/unread-fixture.py /tmp/natterwire-unread
Run the API with --db /tmp/natterwire-unread/chat.db --contacts '' --pins ''.
"""
import pathlib
import sqlite3
import sys
import time

directory = pathlib.Path(sys.argv[1])
directory.mkdir(parents=True, exist_ok=True)
path = directory / "chat.db"
if path.exists():
    raise SystemExit("Use a fresh fixture directory; refusing to overwrite a database")
db = sqlite3.connect(path)
db.executescript((pathlib.Path(__file__).parents[1] / "testdata/messages.sql").read_text())
db.execute("PRAGMA journal_mode=WAL")
db.execute("UPDATE chat SET display_name='Incoming every 30s (test)' WHERE ROWID=2")
db.execute("DELETE FROM chat_handle_join WHERE chat_id=2")


def incoming(text):
    row = db.execute(
        "INSERT INTO message (guid,text,date,is_from_me,is_read,service) VALUES (?,?,?,?,?,?)",
        (f"test-{time.time_ns()}", text, time.time_ns() - 978307200000000000, 0, 0, "iMessage"),
    ).lastrowid
    db.execute("INSERT INTO chat_message_join VALUES (2,?)", (row,))
    db.commit()


for number in range(40):
    incoming(f"Test history {number + 1}. Scroll up to keep new arrivals unread.")
print(f"Ready: {path}. Incoming messages every 30 seconds; stop this service to end.", flush=True)
number = 0
while True:
    time.sleep(30)
    number += 1
    incoming(f"Incoming test message {number} at {time.strftime('%H:%M:%S')} UTC")
    print(f"Inserted incoming test message {number}", flush=True)
