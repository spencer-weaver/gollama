#!/usr/bin/env python3
"""
Synthesise text using piper-tts and play it through an ALSA device.
Compatible with piper-tts >= 1.4.0 (OHF piper1 API).

Usage: tts.py <text> --model /path/to/voice.onnx [--device plughw:2,0]
"""
import sys
import argparse
import subprocess


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("text", help="text to speak")
    parser.add_argument("--model", required=True, help="path to .onnx voice model")
    parser.add_argument("--device", default="", help="ALSA playback device (e.g. plughw:2,0)")
    args = parser.parse_args()

    text = args.text.strip()
    if not text:
        return

    try:
        from piper import PiperVoice
    except ImportError:
        print("ERROR: piper-tts not installed", file=sys.stderr)
        sys.exit(1)

    voice = PiperVoice.load(args.model)

    # Collect all int16 PCM bytes from the AudioChunk iterator.
    pcm = b""
    sample_rate = None
    for chunk in voice.synthesize(text):
        pcm += chunk.audio_int16_bytes
        if sample_rate is None:
            sample_rate = chunk.sample_rate

    if not pcm or sample_rate is None:
        print("WARNING: piper produced no audio", file=sys.stderr)
        sys.exit(0)

    # Build a minimal WAV in memory and pipe to aplay.
    import struct
    data_size = len(pcm)
    header = struct.pack("<4sI4s4sIHHIIHH4sI",
        b"RIFF", 36 + data_size, b"WAVE",
        b"fmt ", 16,
        1,               # PCM
        1,               # mono
        sample_rate,
        sample_rate * 2, # byte rate
        2,               # block align
        16,              # bits per sample
        b"data", data_size,
    )
    wav = header + pcm

    aplay_cmd = ["aplay", "-q"]
    if args.device:
        aplay_cmd += ["-D", args.device]
    aplay_cmd.append("-")

    result = subprocess.run(aplay_cmd, input=wav)
    if result.returncode != 0:
        print(f"aplay failed (exit {result.returncode})", file=sys.stderr)
    sys.exit(result.returncode)


if __name__ == "__main__":
    main()
