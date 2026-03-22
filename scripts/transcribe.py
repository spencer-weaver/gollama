#!/usr/bin/env python3
"""
Transcribe a WAV file using faster-whisper and print the text to stdout.
Usage: transcribe.py <audio_file> [--model base.en] [--language en]
"""
import sys
import argparse

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("audio", help="path to WAV file")
    parser.add_argument("--model", default="base.en", help="whisper model name")
    parser.add_argument("--language", default="en", help="language code")
    args = parser.parse_args()

    try:
        from faster_whisper import WhisperModel
    except ImportError:
        print("ERROR: faster_whisper not installed in this environment", file=sys.stderr)
        sys.exit(1)

    model = WhisperModel(args.model, device="cpu", compute_type="int8")
    segments, _ = model.transcribe(args.audio, language=args.language, beam_size=5)

    parts = []
    for seg in segments:
        text = seg.text.strip()
        if text:
            parts.append(text)

    print(" ".join(parts))

if __name__ == "__main__":
    main()
