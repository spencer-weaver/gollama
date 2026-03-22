package voice

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)


// TTSBackend identifies which text-to-speech engine to use.
type TTSBackend string

const (
	TTSPiper        TTSBackend = "piper"     // neural TTS via venv or system piper binary
	TTSEspeak       TTSBackend = "espeak-ng" // apt install espeak-ng (fast, robotic fallback)
	TTSEspeakLegacy TTSBackend = "espeak"
)

// TTS wraps a local text-to-speech engine.
type TTS struct {
	Backend        TTSBackend
	Voice          string // for espeak: voice name e.g. "en-us"; unused for piper (uses scriptPath)
	PlaybackDevice string // ALSA playback device passed to aplay -D (empty = default)
	Debug          bool
	scriptPath     string // absolute path to scripts/tts.py (for venv piper)
	pythonPath     string // absolute path to .venv/bin/python3
	modelPath      string // absolute path to .onnx voice model
}

// ModelPath returns the voice model path (for diagnostics).
func (t *TTS) ModelPath() string { return t.modelPath }

// DefaultTTS returns a TTS configured with the best available backend.
// Probe order:
//  1. Project venv (.venv) with piper-tts installed + a downloaded voice model
//  2. System piper binary on PATH
//  3. espeak-ng / espeak on PATH (robotic fallback)
//
// projectRoot is the resolved gollama project directory.
func DefaultTTS(projectRoot string) (*TTS, error) {
	venvPython := filepath.Join(projectRoot, ".venv", "bin", "python3")
	scriptPath := filepath.Join(projectRoot, "scripts", "tts.py")
	ttsDir := filepath.Join(projectRoot, "models", "tts")

	// 1. Venv piper + downloaded voice model.
	if _, err := os.Stat(venvPython); err == nil {
		if _, err := os.Stat(scriptPath); err == nil {
			if model := findVoiceModel(ttsDir); model != "" {
				return &TTS{
					Backend:    TTSPiper,
					scriptPath: scriptPath,
					pythonPath: venvPython,
					modelPath:  model,
				}, nil
			}
		}
	}

	// 2. System piper binary.
	if _, err := exec.LookPath("piper"); err == nil {
		if model := findVoiceModel(ttsDir); model != "" {
			return &TTS{Backend: TTSPiper, modelPath: model}, nil
		}
	}

	// 3. Espeak fallback.
	for _, backend := range []TTSBackend{TTSEspeak, TTSEspeakLegacy} {
		if _, err := exec.LookPath(string(backend)); err == nil {
			return &TTS{Backend: backend, Voice: "en-us"}, nil
		}
	}

	return nil, fmt.Errorf(
		"no TTS backend found\n\n"+
			"Install piper (recommended):\n"+
			"  cd %s\n"+
			"  .venv/bin/pip install piper-tts\n"+
			"  mkdir -p models/tts && cd models/tts\n"+
			"  curl -LO https://huggingface.co/rhasspy/piper-voices/resolve/v1.0.0/en/en_US/lessac/high/en_US-lessac-high.onnx\n"+
			"  curl -LO https://huggingface.co/rhasspy/piper-voices/resolve/v1.0.0/en/en_US/lessac/high/en_US-lessac-high.onnx.json\n\n"+
			"Or install espeak-ng (robotic fallback):\n"+
			"  apt install espeak-ng",
		projectRoot,
	)
}

// FindPiperModel returns the path to the first .onnx file found in dir.
// Exported so callers can pass the model path to external tools.
func FindPiperModel(dir string) string {
	return findVoiceModel(dir)
}

// findVoiceModel returns the path to the first .onnx file found in dir.
func findVoiceModel(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".onnx") {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// Speak synthesises text and plays it. Blocks until playback is complete.
func (t *TTS) Speak(text string) error {
	text = cleanForSpeech(text)
	if text == "" {
		return nil
	}
	switch t.Backend {
	case TTSPiper:
		return t.speakPiper(text)
	case TTSEspeak, TTSEspeakLegacy:
		return t.speakEspeak(text)
	default:
		return fmt.Errorf("unknown TTS backend: %s", t.Backend)
	}
}

// SpeakStream reads text chunks from the provided generator, buffers into
// sentences, and speaks each sentence as soon as it's complete. This lets
// speech begin while the model is still generating the rest of its response.
// Returns the full response text.
func (t *TTS) SpeakStream(fn func(write func(chunk string))) (string, error) {
	sentCh := make(chan string, 8)
	var full strings.Builder

	go func() {
		defer close(sentCh)
		var buf strings.Builder
		fn(func(chunk string) {
			full.WriteString(chunk)
			buf.WriteString(chunk)
			for {
				s := buf.String()
				idx := sentenceBoundary(s)
				if idx < 0 {
					break
				}
				sentence := strings.TrimSpace(s[:idx+1])
				buf.Reset()
				buf.WriteString(s[idx+1:])
				if sentence != "" {
					sentCh <- sentence
				}
			}
		})
		if remaining := strings.TrimSpace(buf.String()); remaining != "" {
			sentCh <- remaining
		}
	}()

	for sentence := range sentCh {
		if err := t.Speak(sentence); err != nil {
			fmt.Printf("[tts error: %v]\n", err)
		}
	}

	return full.String(), nil
}

func sentenceBoundary(s string) int {
	for i, ch := range s {
		if ch == '.' || ch == '!' || ch == '?' {
			next := i + 1
			if next >= len(s) || s[next] == ' ' || s[next] == '\n' {
				return i
			}
		}
	}
	return -1
}

func (t *TTS) speakPiper(text string) error {
	if t.pythonPath != "" && t.scriptPath != "" {
		// venv piper via scripts/tts.py
		args := []string{t.scriptPath, text, "--model", t.modelPath}
		if t.PlaybackDevice != "" {
			args = append(args, "--device", t.PlaybackDevice)
		}
		if t.Debug {
			fmt.Fprintf(os.Stderr, "[tts] piper: %q\n", text)
		}
		cmd := exec.Command(t.pythonPath, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("piper tts: %w\n%s", err, string(out))
		}
		return nil
	}
	// system piper binary
	piper := exec.Command("piper",
		"--model", t.modelPath,
		"--output-raw",
	)
	piper.Stdin = strings.NewReader(text)
	piperOut, err := piper.StdoutPipe()
	if err != nil {
		return err
	}
	if err := piper.Start(); err != nil {
		return fmt.Errorf("piper: %w", err)
	}
	aplayArgs := []string{"-r", "22050", "-f", "S16_LE", "-c", "1", "-q", "-"}
	if t.PlaybackDevice != "" {
		aplayArgs = append([]string{"-D", t.PlaybackDevice}, aplayArgs...)
	}
	aplay := exec.Command("aplay", aplayArgs...)
	aplay.Stdin = piperOut
	if err := aplay.Run(); err != nil {
		piper.Wait()
		return fmt.Errorf("aplay: %w", err)
	}
	return piper.Wait()
}

func (t *TTS) speakEspeak(text string) error {
	voice := t.Voice
	if voice == "" {
		voice = "en-us"
	}
	if err := exec.Command(string(t.Backend), "-v", voice, text).Run(); err != nil {
		return fmt.Errorf("%s: %w", t.Backend, err)
	}
	return nil
}

var reMarkdown = regexp.MustCompile(`(?m)[*_` + "`" + `#]+|\[([^\]]+)\]\([^)]+\)`)
var reMultiSpace = regexp.MustCompile(`\s+`)

func cleanForSpeech(s string) string {
	s = reMarkdown.ReplaceAllString(s, "$1")
	s = reMultiSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
