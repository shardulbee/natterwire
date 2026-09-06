"""Measure key injection to completed frame output, not the browser transport."""
import argparse
import os
from pathlib import Path
import statistics
import time
from smoke import Terminal


parser = argparse.ArgumentParser()
parser.add_argument('binary', type=lambda p: str(Path(p).resolve()))
args = parser.parse_args()
terminal = Terminal(args.binary, ['--demo'])
try:
    terminal.expect('Meet at the coffee shop')
    terminal.resize(160, 60)
    terminal.read(0.2)
    original_read = os.read
    frames = []
    tail = b''

    def read(fd, size):
        global tail
        data = original_read(fd, size)
        if b'\x1b[?2026l' in tail + data:
            frames.append(time.perf_counter())
        tail = (tail + data)[-7:]
        return data

    os.read = read
    samples = []
    for i in range(60):
        frames.clear()
        started = time.perf_counter()
        os.write(terminal.fd, b'J' if i % 2 == 0 else b'K')
        while not frames and time.perf_counter() - started < 1:
            terminal.read(0.001)
        assert frames, 'No selection frame within one second'
        samples.append((frames[0] - started) * 1000)
    print('160x60 local PTY key-to-frame: median %.2fms, p95 %.2fms, max %.2fms' % (
        statistics.median(samples), sorted(samples)[56], max(samples)))
finally:
    terminal.close()
