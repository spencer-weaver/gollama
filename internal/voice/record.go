// Package voice provides audio recording, speech-to-text, and text-to-speech
// primitives for the voice brainstorm mode.
package voice

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"time"
)

const (
	sampleRate      = 16000 // Hz — matches whisper's expected input
	channels        = 1     // mono
	bitsPerSample   = 16    // signed little-endian int16
	chunkDuration   = 80    // ms per analysis window
	samplesPerChunk = sampleRate * chunkDuration / 1000
	bytesPerChunk   = samplesPerChunk * (bitsPerSample / 8) * channels

	warmupChunks      = 10 // ~800ms discarded on startup (ALSA buffer artefact)
	calibrationChunks = 15 // ~1.2s of ambient noise sampling
)

// RecordConfig controls silence detection and audio device selection.
type RecordConfig struct {
	// Device is the ALSA capture device string passed to arecord -D.
	Device string
	// SilenceDuration is how long energy must stay below the threshold
	// (after speech has started) before recording stops. Default: 1.5s.
	SilenceDuration time.Duration
	// MaxDuration caps the length of a single utterance. Default: 60s.
	MaxDuration time.Duration
	// Debug prints live VAD state and energy levels to stderr.
	Debug bool
}

func DefaultRecordConfig() RecordConfig {
	return RecordConfig{
		Device:          "plughw:2,0",
		SilenceDuration: 1500 * time.Millisecond,
		MaxDuration:     60 * time.Second,
	}
}

// vadThresholds derives speech-start and silence-end thresholds from a
// measured noise floor. Handles the full dynamic range:
//
//	Quiet mic  (floor≈0.005): speak>0.015  silence<0.007
//	Normal mic (floor≈0.187): speak>0.374  silence<0.243
//	High-gain  (floor≈0.987): speak>0.980  silence<0.989 (capped)
func vadThresholds(noiseFloor float64) (speechThreshold, silenceThreshold float64) {
	speechThreshold = noiseFloor * 3.0
	if speechThreshold > 0.95 {
		speechThreshold = math.Min(noiseFloor+0.05, 0.98)
	}
	silenceThreshold = noiseFloor + (speechThreshold-noiseFloor)*0.3
	return
}

// Recorder holds a single persistent arecord process and VAD state.
// Create one with NewRecorder and call ReadUtterance in a loop.
// Call Close when done.
type Recorder struct {
	cfg    RecordConfig
	cmd    *exec.Cmd
	stdout io.ReadCloser
	chunk  []byte

	// calibrated after warmup
	noiseFloor       float64
	speechThreshold  float64
	silenceThreshold float64
	calibrated       bool
}

// NewRecorder starts arecord and performs warmup + noise-floor calibration.
// The arecord process runs until Close is called.
func NewRecorder(cfg RecordConfig) (*Recorder, error) {
	device := cfg.Device
	if device == "" {
		device = "plughw:2,0"
	}

	cmd := exec.Command("arecord",
		"-D", device,
		"-f", "S16_LE",
		"-r", fmt.Sprintf("%d", sampleRate),
		"-c", fmt.Sprintf("%d", channels),
		"-q",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start arecord: %w (is arecord installed?)", err)
	}

	r := &Recorder{
		cfg:    cfg,
		cmd:    cmd,
		stdout: stdout,
		chunk:  make([]byte, bytesPerChunk),
	}

	if err := r.calibrate(&stderrBuf); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

// Close stops the arecord process.
func (r *Recorder) Close() {
	if r.cmd != nil && r.cmd.Process != nil {
		r.cmd.Process.Kill()
		r.cmd.Wait()
	}
}

// calibrate discards warmup chunks then measures the noise floor.
func (r *Recorder) calibrate(stderrBuf *bytes.Buffer) error {
	if r.cfg.Debug {
		fmt.Fprintf(os.Stderr, "[vad] warming up (%d chunks)...\n", warmupChunks)
	}
	// Discard ALSA init burst.
	for i := 0; i < warmupChunks; i++ {
		if _, err := io.ReadFull(r.stdout, r.chunk); err != nil {
			if msg := stderrBuf.String(); msg != "" {
				return fmt.Errorf("arecord error: %s", msg)
			}
			return fmt.Errorf("arecord warmup read: %w", err)
		}
		if r.cfg.Debug {
			fmt.Fprintf(os.Stderr, "\r[vad] warmup %d/%d    ", i+1, warmupChunks)
		}
	}

	// Calibrate noise floor.
	var samples []float64
	for i := 0; i < calibrationChunks; i++ {
		if _, err := io.ReadFull(r.stdout, r.chunk); err != nil {
			return fmt.Errorf("arecord calibration read: %w", err)
		}
		rms := rmsEnergy(r.chunk)
		samples = append(samples, rms)
		if r.cfg.Debug {
			fmt.Fprintf(os.Stderr, "\r[vad] calibrating  rms=%.4f (%d/%d)    ", rms, i+1, calibrationChunks)
		}
	}

	r.noiseFloor = avgFloat(samples)
	r.speechThreshold, r.silenceThreshold = vadThresholds(r.noiseFloor)
	r.calibrated = true

	if r.cfg.Debug {
		fmt.Fprintf(os.Stderr, "\n[vad] floor=%.4f  speak>%.4f  silence<%.4f\n",
			r.noiseFloor, r.speechThreshold, r.silenceThreshold)
	}
	return nil
}

type vadState int

const (
	vadWaiting   vadState = iota // waiting for speech
	vadCapturing                 // capturing utterance
)

// ReadUtterance blocks until a complete utterance is captured or MaxDuration
// elapses. Returns raw S16_LE PCM bytes. Returns empty slice (no error) if
// MaxDuration elapsed without speech.
func (r *Recorder) ReadUtterance() ([]byte, error) {
	var (
		state         = vadWaiting
		silenceStart  time.Time
		started       = time.Now()
		speechBuf     bytes.Buffer
		preSpeechRing [][]byte
	)

	for {
		if state == vadCapturing && time.Since(started) > r.cfg.MaxDuration {
			if r.cfg.Debug {
				fmt.Fprintf(os.Stderr, "\n[vad] max duration reached\n")
			}
			break
		}
		// When waiting, also respect MaxDuration as an overall timeout.
		if state == vadWaiting && time.Since(started) > r.cfg.MaxDuration {
			break
		}

		n, readErr := io.ReadFull(r.stdout, r.chunk)
		if readErr != nil && n == 0 {
			if r.cfg.Debug {
				fmt.Fprintf(os.Stderr, "\n[vad] pipe closed: %v\n", readErr)
			}
			return nil, fmt.Errorf("arecord pipe closed unexpectedly: %w", readErr)
		}
		data := append([]byte(nil), r.chunk[:n]...)
		rms := rmsEnergy(data)

		switch state {
		case vadWaiting:
			preSpeechRing = append(preSpeechRing, data)
			if len(preSpeechRing) > 3 {
				preSpeechRing = preSpeechRing[1:]
			}
			if r.cfg.Debug {
				fmt.Fprintf(os.Stderr, "\r[vad] waiting   rms=%.4f  speak>%.4f    ", rms, r.speechThreshold)
			}
			if rms > r.speechThreshold {
				state = vadCapturing
				for _, pre := range preSpeechRing {
					speechBuf.Write(pre)
				}
				speechBuf.Write(data)
				silenceStart = time.Time{}
				if r.cfg.Debug {
					fmt.Fprintf(os.Stderr, "\n[vad] *** SPEECH ***\n")
				}
			}

		case vadCapturing:
			speechBuf.Write(data)
			if r.cfg.Debug {
				fmt.Fprintf(os.Stderr, "\r[vad] capturing rms=%.4f  silence<%.4f    ", rms, r.silenceThreshold)
			}
			if rms < r.silenceThreshold {
				if silenceStart.IsZero() {
					silenceStart = time.Now()
				} else if time.Since(silenceStart) >= r.cfg.SilenceDuration {
					if r.cfg.Debug {
						fmt.Fprintf(os.Stderr, "\n[vad] silence — done\n")
					}
					return speechBuf.Bytes(), nil
				}
			} else {
				silenceStart = time.Time{}
			}
		}
	}

	return speechBuf.Bytes(), nil
}

// RecordToFile calls ReadUtterance and writes the result as a WAV file.
// Returns "" (no error) if no speech was detected.
// The caller must os.Remove the returned path when done.
func (r *Recorder) RecordToFile() (string, error) {
	pcm, err := r.ReadUtterance()
	if err != nil {
		return "", err
	}
	if len(pcm) == 0 {
		return "", nil
	}

	f, err := os.CreateTemp("", "gollama-voice-*.wav")
	if err != nil {
		return "", fmt.Errorf("create temp WAV: %w", err)
	}
	path := f.Name()
	f.Close()

	if err := writeWAV(path, pcm); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// rmsEnergy computes the normalised RMS of a chunk of S16_LE PCM samples.
func rmsEnergy(data []byte) float64 {
	if len(data) < 2 {
		return 0
	}
	var sum float64
	n := len(data) / 2
	for i := 0; i < n; i++ {
		sample := int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
		v := float64(sample) / 32768.0
		sum += v * v
	}
	return math.Sqrt(sum / float64(n))
}

func avgFloat(s []float64) float64 {
	var sum float64
	for _, v := range s {
		sum += v
	}
	return sum / float64(len(s))
}

// writeWAV writes raw S16_LE PCM data into a valid WAV file.
func writeWAV(path string, pcm []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dataSize := uint32(len(pcm))
	byteRate := uint32(sampleRate * channels * bitsPerSample / 8)
	blockAlign := uint16(channels * bitsPerSample / 8)

	writeBytes(f, []byte("RIFF"))
	writeUint32LE(f, 36+dataSize)
	writeBytes(f, []byte("WAVE"))
	writeBytes(f, []byte("fmt "))
	writeUint32LE(f, 16)
	writeUint16LE(f, 1)
	writeUint16LE(f, uint16(channels))
	writeUint32LE(f, uint32(sampleRate))
	writeUint32LE(f, byteRate)
	writeUint16LE(f, blockAlign)
	writeUint16LE(f, uint16(bitsPerSample))
	writeBytes(f, []byte("data"))
	writeUint32LE(f, dataSize)
	_, err = f.Write(pcm)
	return err
}

func writeBytes(w io.Writer, b []byte)    { w.Write(b) }
func writeUint32LE(w io.Writer, v uint32) { binary.Write(w, binary.LittleEndian, v) }
func writeUint16LE(w io.Writer, v uint16) { binary.Write(w, binary.LittleEndian, v) }
