#!/usr/bin/env python3
"""
voice_pipe.py — STT subprocess for gollama.

Pipeline:
  arecord → 500ms VAD windows (silero via faster-whisper) → Whisper

VAD strategy:
  - Analyse audio in 500ms windows (silero needs longer context than 30ms)
  - "speech" window: at least one speech timestamp found by silero
  - "silent" window: no speech timestamps AND low RMS energy (dual check)
  - Utterance ends when END_SILENCE_SECS of consecutive silent windows seen

Protocol (JSON lines):
  Go → Python stdin:   {"cmd": "mute"} / {"cmd": "unmute"} / {"cmd": "quit"}
  Python → Go stdout:  {"event": "ready"} / {"event": "transcript", "text": "..."}
                        {"event": "error", "msg": "..."}
"""
import sys
import json
import argparse
import subprocess
import threading
import time
import os
import math

import numpy as np

SAMPLE_RATE     = 16000
VAD_WINDOW_SECS = 0.5                                  # analyse in 500ms windows
VAD_WINDOW_SAMPLES = int(SAMPLE_RATE * VAD_WINDOW_SECS)
VAD_WINDOW_BYTES   = VAD_WINDOW_SAMPLES * 2            # int16

END_SILENCE_SECS = 1.2   # consecutive silence before committing utterance
MIN_SPEECH_SECS  = 0.3   # ignore utterances shorter than this
MAX_SPEECH_SECS  = 30.0  # hard cap


def emit(obj):
    print(json.dumps(obj), flush=True)


def log(msg):
    print(f"[stt] {msg}", file=sys.stderr, flush=True)


def rms(pcm: bytes) -> float:
    a = np.frombuffer(pcm, dtype=np.int16).astype(np.float32) / 32768.0
    return float(math.sqrt(np.mean(a * a)))


def to_float32(pcm: bytes) -> np.ndarray:
    return np.frombuffer(pcm, dtype=np.int16).astype(np.float32) / 32768.0


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", default="base.en")
    parser.add_argument("--device", default="plughw:2,0")
    parser.add_argument("--debug", action="store_true")
    args = parser.parse_args()

    # ── Load models ────────────────────────────────────────────────────────────
    try:
        from faster_whisper import WhisperModel
        from faster_whisper.vad import VadOptions, get_speech_timestamps
    except ImportError as e:
        emit({"event": "error", "msg": f"faster-whisper not installed: {e}"})
        sys.exit(1)

    if args.debug:
        log(f"loading whisper {args.model!r}...")
    whisper = WhisperModel(args.model, device="cpu", compute_type="int8")

    vad_opts = VadOptions(
        threshold=0.4,
        neg_threshold=0.3,
        min_speech_duration_ms=200,
        min_silence_duration_ms=200,
        speech_pad_ms=100,
    )
    if args.debug:
        log("ready")

    # ── Mute state ─────────────────────────────────────────────────────────────
    muted = threading.Event()
    mute_until = [0.0]

    # ── Start arecord (with retry if device busy) ──────────────────────────────
    def start_arecord():
        for attempt in range(10):
            proc = subprocess.Popen(
                ["arecord", "-D", args.device,
                 "-f", "S16_LE", "-r", str(SAMPLE_RATE), "-c", "1", "-q"],
                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            )
            # Give it 200ms and check if it errored immediately.
            time.sleep(0.2)
            if proc.poll() is not None:
                err = proc.stderr.read().decode().strip()
                if args.debug:
                    log(f"arecord failed (attempt {attempt+1}): {err}")
                time.sleep(1.0)
                continue
            if args.debug:
                log(f"arecord started on {args.device}")
            return proc
        emit({"event": "error",
              "msg": f"arecord could not open {args.device} after 10 attempts. "
                     "Run: pkill -f arecord"})
        sys.exit(1)

    proc = start_arecord()

    # ── Command reader ─────────────────────────────────────────────────────────
    def cmd_reader():
        for line in sys.stdin:
            line = line.strip()
            if not line:
                continue
            try:
                msg = json.loads(line)
            except json.JSONDecodeError:
                continue
            cmd = msg.get("cmd", "")
            if cmd == "mute":
                muted.set()
                if args.debug:
                    log("muted")
            elif cmd == "unmute":
                muted.clear()
                mute_until[0] = time.time() + 1.0
                if args.debug:
                    log("unmuted (1s echo)")
            elif cmd == "quit":
                proc.kill()
                os._exit(0)

    threading.Thread(target=cmd_reader, daemon=True).start()

    emit({"event": "ready"})

    # ── VAD + accumulation loop ────────────────────────────────────────────────
    speech_pcm: list[bytes] = []
    silent_secs  = 0.0
    in_speech    = False
    speech_start = 0.0

    # Noise floor calibration — done ONLY when unmuted (not during TTS playback).
    # Calibrating while speakers are on gives floor≈0.97 and makes real speech
    # look like silence. Reset and re-calibrate after every mute/unmute cycle.
    noise_floor = 0.02
    calib_samples: list[float] = []
    calibrated = False
    CALIB_WINDOWS = 4   # 2 seconds of ambient noise before listening

    while True:
        # Read one VAD window.
        pcm = b""
        while len(pcm) < VAD_WINDOW_BYTES:
            chunk = proc.stdout.read(VAD_WINDOW_BYTES - len(pcm))
            if not chunk:
                err = proc.stderr.read().decode().strip()
                if err:
                    emit({"event": "error", "msg": f"arecord closed: {err}"})
                return
            pcm += chunk

        # While muted: discard audio and reset calibration so the next
        # unmute period starts with a fresh noise-floor measurement.
        if muted.is_set() or time.time() < mute_until[0]:
            speech_pcm.clear()
            in_speech = False
            silent_secs = 0.0
            calibrated = False
            calib_samples.clear()
            continue

        # Calibrate noise floor on first CALIB_WINDOWS of unmuted audio.
        if not calibrated:
            calib_samples.append(rms(pcm))
            if args.debug:
                log(f"calibrating {len(calib_samples)}/{CALIB_WINDOWS} "
                    f"rms={calib_samples[-1]:.4f}")
            if len(calib_samples) >= CALIB_WINDOWS:
                noise_floor = max(sum(calib_samples) / len(calib_samples), 0.005)
                calibrated = True
                if args.debug:
                    log(f"noise floor = {noise_floor:.4f}  "
                        f"speech threshold = {noise_floor * 2.0:.4f}")
            continue

        # ── VAD decision for this window ──────────────────────────────────────
        energy = rms(pcm)
        audio  = to_float32(pcm)

        try:
            ts = get_speech_timestamps(audio, vad_opts, sampling_rate=SAMPLE_RATE)
            silero_speech = len(ts) > 0
        except Exception:
            silero_speech = False

        # Energy threshold: 2× noise floor OR silero says speech.
        energy_speech = energy > noise_floor * 2.0
        is_speech = silero_speech or energy_speech

        if args.debug:
            marker = "▶" if is_speech else "·"
            log(f"{marker} rms={energy:.3f} floor={noise_floor:.3f} "
                f"silero={'Y' if silero_speech else 'N'} "
                f"energy={'Y' if energy_speech else 'N'}")

        now = time.time()

        if is_speech:
            if not in_speech:
                in_speech = True
                speech_start = now
                if args.debug:
                    log("speech START")
            speech_pcm.append(pcm)
            silent_secs = 0.0

            if now - speech_start >= MAX_SPEECH_SECS:
                _transcribe(whisper, speech_pcm, args.debug)
                speech_pcm.clear()
                in_speech = False
        else:
            if in_speech:
                speech_pcm.append(pcm)  # keep tail
                silent_secs += VAD_WINDOW_SECS
                if args.debug:
                    log(f"silence {silent_secs:.1f}/{END_SILENCE_SECS}s")
                if silent_secs >= END_SILENCE_SECS:
                    duration = now - speech_start
                    if args.debug:
                        log(f"speech END ({duration:.1f}s)")
                    if duration >= MIN_SPEECH_SECS:
                        _transcribe(whisper, speech_pcm, args.debug)
                    speech_pcm.clear()
                    in_speech = False
                    silent_secs = 0.0
            else:
                # Slowly re-calibrate noise floor during silence.
                noise_floor = noise_floor * 0.95 + energy * 0.05


def _transcribe(whisper, frames: list[bytes], debug: bool):
    pcm = b"".join(frames)
    audio = np.frombuffer(pcm, dtype=np.int16).astype(np.float32) / 32768.0
    try:
        segments, _ = whisper.transcribe(
            audio,
            language="en",
            beam_size=5,
            vad_filter=False,
            condition_on_previous_text=False,
            no_speech_threshold=0.6,
            compression_ratio_threshold=2.4,
            temperature=0.0,
        )
        text = " ".join(
            s.text for s in segments if s.no_speech_prob < 0.6
        ).strip()
    except Exception as e:
        if debug:
            log(f"whisper error: {e}")
        return
    if debug:
        log(f"transcript: {text!r}")
    if text:
        emit({"event": "transcript", "text": text})


if __name__ == "__main__":
    main()
