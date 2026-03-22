package voice

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// STTBackend identifies which speech-to-text tool to use.
type STTBackend string

const (
	STTFasterWhisper STTBackend = "faster-whisper" // via venv or system
	STTWhisper       STTBackend = "whisper"         // openai-whisper system install
)

// STT wraps a local speech-to-text tool.
type STT struct {
	Backend    STTBackend
	Model      string // e.g. "tiny.en", "base.en", "small.en"
	scriptPath string // absolute path to transcribe.py (for venv backend)
	pythonPath string // absolute path to python3 binary inside venv
}

// DefaultSTT returns an STT ready to transcribe. It probes for available
// backends in this order:
//  1. .venv inside the gollama project root (faster-whisper via transcribe.py)
//  2. faster-whisper binary on PATH
//  3. whisper binary on PATH
//
// projectRoot is the resolved gollama project directory (e.g. .../cli/gollama).
func DefaultSTT(projectRoot, model string) (*STT, error) {
	if model == "" {
		model = "base.en"
	}

	// 1. Check for the project-local venv with faster-whisper installed.
	venvPython := filepath.Join(projectRoot, ".venv", "bin", "python3")
	scriptPath := filepath.Join(projectRoot, "scripts", "transcribe.py")
	if _, err := os.Stat(venvPython); err == nil {
		if _, err := os.Stat(scriptPath); err == nil {
			return &STT{
				Backend:    STTFasterWhisper,
				Model:      model,
				scriptPath: scriptPath,
				pythonPath: venvPython,
			}, nil
		}
	}

	// 2. faster-whisper binary on PATH.
	if p, err := exec.LookPath("faster-whisper"); err == nil {
		_ = p
		return &STT{Backend: STTFasterWhisper, Model: model}, nil
	}

	// 3. whisper binary on PATH.
	if _, err := exec.LookPath("whisper"); err == nil {
		return &STT{Backend: STTWhisper, Model: model}, nil
	}

	return nil, fmt.Errorf(
		"no speech-to-text tool found\n\n" +
			"The project venv is missing faster-whisper. Re-run:\n" +
			"  cd %s\n" +
			"  python3 -m venv .venv\n" +
			"  .venv/bin/pip install faster-whisper\n\n" +
			"Or install system-wide:\n" +
			"  pipx install openai-whisper",
		projectRoot,
	)
}

// Transcribe converts the audio file at audioPath to text.
func (s *STT) Transcribe(audioPath string) (string, error) {
	switch s.Backend {
	case STTFasterWhisper:
		if s.pythonPath != "" {
			return s.runVenv(audioPath)
		}
		return s.runFasterWhisperBin(audioPath)
	case STTWhisper:
		return s.runWhisper(audioPath)
	default:
		return "", fmt.Errorf("unknown STT backend: %s", s.Backend)
	}
}

// runVenv invokes transcribe.py using the project venv's python3.
func (s *STT) runVenv(audioPath string) (string, error) {
	out, err := exec.Command(s.pythonPath, s.scriptPath,
		audioPath,
		"--model", s.Model,
		"--language", "en",
	).Output()
	if err != nil {
		// include stderr for easier debugging
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("transcribe: %w\n%s", err, string(ee.Stderr))
		}
		return "", fmt.Errorf("transcribe: %w", err)
	}
	return cleanTranscript(string(out)), nil
}

// runFasterWhisperBin calls a system-installed faster-whisper CLI binary.
func (s *STT) runFasterWhisperBin(audioPath string) (string, error) {
	out, err := exec.Command("faster-whisper",
		"--model", s.Model,
		"--language", "en",
		"--output-format", "txt",
		audioPath,
	).Output()
	if err != nil {
		return "", fmt.Errorf("faster-whisper: %w", err)
	}
	return cleanTranscript(string(out)), nil
}

// runWhisper calls the openai-whisper CLI.
func (s *STT) runWhisper(audioPath string) (string, error) {
	out, err := exec.Command("whisper",
		audioPath,
		"--model", s.Model,
		"--language", "en",
		"--output_format", "txt",
		"--verbose", "False",
	).Output()
	if err != nil {
		return "", fmt.Errorf("whisper: %w", err)
	}
	return cleanTranscript(string(out)), nil
}

// cleanTranscript normalises STT output.
func cleanTranscript(raw string) string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// skip whisper timestamp lines like "[00:00.000 --> 00:03.200]"
		if strings.HasPrefix(line, "[") && strings.Contains(line, "-->") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, " ")
}
