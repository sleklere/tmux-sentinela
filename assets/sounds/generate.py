#!/usr/bin/env python3
"""Generate tmux-sentinela's original completion chime."""

import math
import struct
import wave
from pathlib import Path

RATE = 48_000
DURATION = 0.82
NOTES = (
    (0.0, 587.33, 0.28, -0.10),
    (0.115, 783.99, 0.34, 0.10),
)
HARMONICS = (
    (1, 1.0, 0.0),
    (2, 0.045, 0.2),
)
REVERB = (
    (0.045, 0.07),
    (0.082, 0.035),
)


def generate():
    count = int(RATE * DURATION)
    left = [0.0] * count
    right = [0.0] * count

    for start, frequency, gain, pan in NOTES:
        begin = int(start * RATE)
        for i in range(begin, count):
            elapsed = (i - begin) / RATE
            attack = math.sin(min(1.0, elapsed / 0.014) * math.pi / 2) ** 2
            envelope = attack * math.exp(-7.2 * elapsed)
            tone = sum(
                amplitude
                * math.sin(2 * math.pi * frequency * multiple * elapsed + phase)
                for multiple, amplitude, phase in HARMONICS
            )
            signal = gain * envelope * tone
            left[i] += signal * math.sqrt((1 - pan) / 2)
            right[i] += signal * math.sqrt((1 + pan) / 2)

    for delay, amount in REVERB:
        samples = int(delay * RATE)
        previous_left = left.copy()
        previous_right = right.copy()
        for i in range(samples, count):
            left[i] += previous_left[i - samples] * amount
            right[i] += previous_right[i - samples] * amount

    peak = max(max(map(abs, left)), max(map(abs, right)))
    scale = 0.30 / peak
    frames = bytearray()
    for l, r in zip(left, right):
        frames.extend(struct.pack("<hh", int(l * scale * 32767), int(r * scale * 32767)))
    return frames


path = Path(__file__).with_name("done.wav")
with wave.open(str(path), "wb") as output:
    output.setnchannels(2)
    output.setsampwidth(2)
    output.setframerate(RATE)
    output.writeframes(generate())
