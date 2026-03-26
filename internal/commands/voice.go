package commands

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spencer-weaver/gollama/chat"
	"github.com/spencer-weaver/gollama/internal/config"
	"github.com/spencer-weaver/gollama/internal/session"
	"github.com/spencer-weaver/gollama/internal/voice"
)

var fillerPhrases = []string{
	"Hmm, let me think about that.",
	"Interesting. Give me just a moment.",
	"Got it, thinking through that now.",
	"I see. Let me consider that.",
}

type pipeMsg struct {
	Cmd   string `json:"cmd,omitempty"`
	Event string `json:"event,omitempty"`
	Text  string `json:"text,omitempty"`
	Msg   string `json:"msg,omitempty"`
}

// VoiceHandler runs an interactive voice brainstorm session.
//
// STT is handled by scripts/voice_pipe.py (RealtimeSTT — Silero VAD +
// faster-whisper). TTS is handled directly in Go via scripts/tts.py + aplay.
func VoiceHandler(args []string) error {
	flags := flag.NewFlagSet("voice", flag.ContinueOnError)
	topic := flags.String("topic", "", "initial brainstorm topic")
	threshold := flags.Int("threshold", 80, "completeness score (0-100) to end session")
	sessionPath := flags.String("session", "", "path to session file")
	defaultSessionsDir := filepath.Join(config.GetGlobalRoot(), "sessions")
	sessionsDir := flags.String("sessions", defaultSessionsDir, "directory for session files")
	sttModel := flags.String("model", "base.en", "STT model: tiny.en, base.en, small.en")
	audioDevice := flags.String("audio-device", "plughw:2,0", "ALSA capture device for mic (see: arecord -l)")
	playbackDevice := flags.String("playback-device", "", "ALSA playback device for TTS (empty=default, e.g. plughw:2,0)")
	debug := flags.Bool("debug", false, "verbose diagnostics")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *topic == "" && len(flags.Args()) > 0 {
		*topic = strings.Join(flags.Args(), " ")
	}

	cfg := config.GetGlobal()
	root := config.GetGlobalRoot()

	voiceMC, err := config.LoadModelConfig(cfg.ModelsDir, "voice")
	if err != nil {
		return fmt.Errorf("voice model config: %w", err)
	}
	scoreMC, err := config.LoadModelConfig(cfg.ModelsDir, "score")
	if err != nil {
		return fmt.Errorf("score model config: %w", err)
	}
	voiceChatCfg := applyModelConfig(cfg, voiceMC)
	scoreChatCfg := applyModelConfig(cfg, scoreMC)

	// ── TTS (Go-side, proven working) ─────────────────────────────────────────
	tts, err := voice.DefaultTTS(root)
	if err != nil {
		return err
	}
	tts.PlaybackDevice = *playbackDevice
	tts.Debug = *debug
	if *debug {
		fmt.Fprintf(os.Stderr, "[debug] TTS: backend=%s model=%s device=%q\n",
			tts.Backend, tts.ModelPath(), *playbackDevice)
	}

	// ── Session ───────────────────────────────────────────────────────────────
	path := *sessionPath
	if path == "" {
		slug := *topic
		if slug == "" {
			slug = "voice-session"
		}
		path = session.DefaultPath(*sessionsDir, slug)
	}
	// Always start a new session for voice — carrying old message history into
	// the model context causes hallucination and topic drift. The session file
	// accumulates the full transcript for scoring/planning, but the model only
	// sees the current run's conversation.
	topicStr := *topic
	if topicStr == "" {
		topicStr = "voice brainstorm"
	}
	sess := session.New(topicStr, *threshold)

	// If a session file exists, inherit the score so we don't re-ask things
	// already covered, but do NOT feed old messages to the model.
	if existing, loadErr := session.Load(path); loadErr == nil {
		sess.Score = existing.Score
		fmt.Printf("Starting fresh conversation (prior score: %d/100)\n", sess.Score)
	}

	// Model history is empty — only current-run exchanges are added.
	var history []chat.Msg

	// ── Start STT subprocess ──────────────────────────────────────────────────
	pipeScript := filepath.Join(root, "scripts", "voice_pipe.py")
	pythonBin := filepath.Join(root, ".venv", "bin", "python3")
	pipeArgs := []string{pipeScript, "--model", *sttModel, "--device", *audioDevice}
	if *debug {
		pipeArgs = append(pipeArgs, "--debug")
	}

	pipe := exec.Command(pythonBin, pipeArgs...)
	pipe.Stderr = os.Stderr
	pipeIn, err := pipe.StdinPipe()
	if err != nil {
		return fmt.Errorf("stt stdin pipe: %w", err)
	}
	pipeOut, err := pipe.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stt stdout pipe: %w", err)
	}
	if err := pipe.Start(); err != nil {
		return fmt.Errorf("start voice_pipe.py: %w\n(pip install faster-whisper)", err)
	}
	defer func() {
		sendPipeCmd(pipeIn, pipeMsg{Cmd: "quit"})
		pipe.Wait()
	}()

	events := make(chan pipeMsg, 16)
	go func() {
		scanner := bufio.NewScanner(pipeOut)
		for scanner.Scan() {
			var m pipeMsg
			if err := json.Unmarshal([]byte(scanner.Text()), &m); err == nil {
				events <- m
			}
		}
		close(events)
	}()

	// speak sends text to TTS, muting STT while speaking to avoid echo.
	speak := func(text string) {
		if text == "" {
			return
		}
		fmt.Printf("Interviewer: %s\n", text)
		sendPipeCmd(pipeIn, pipeMsg{Cmd: "mute"})
		if err := tts.Speak(text); err != nil {
			fmt.Fprintf(os.Stderr, "[tts error: %v]\n", err)
		}
		sendPipeCmd(pipeIn, pipeMsg{Cmd: "unmute"})
	}

	// Wait for STT to be ready.
	fmt.Println("[initialising speech recognition...]")
	for ev := range events {
		if ev.Event == "error" {
			return fmt.Errorf("stt: %s", ev.Msg)
		}
		if ev.Event == "ready" {
			break
		}
	}

	fmt.Printf("\n[voice brainstorm — say 'done', 'finish', or 'quit' to end]\n")
	fmt.Printf("[session: %s | threshold: %d/100]\n\n", path, *threshold)

	// Greet / resume.
	last := sess.LastAssistantMessage()
	if last != "" {
		speak(last)
	} else {
		var firstPrompt string
		if *topic != "" {
			firstPrompt = fmt.Sprintf("I want to brainstorm about: %s", *topic)
		} else {
			firstPrompt = "Hello, I'd like to brainstorm."
		}
		if err := streamSpeak(voiceChatCfg, speak, nil, firstPrompt, sess, &history, path); err != nil {
			return err
		}
	}

	// ── Main loop ─────────────────────────────────────────────────────────────
	for {
		fmt.Print("\n[listening...]\n")

		ev, ok := <-events
		if !ok {
			return fmt.Errorf("STT process exited unexpectedly")
		}
		if ev.Event == "error" {
			fmt.Fprintf(os.Stderr, "[stt error: %s]\n", ev.Msg)
			continue
		}
		if ev.Event != "transcript" {
			continue
		}

		transcript := strings.TrimSpace(ev.Text)
		if transcript == "" {
			continue
		}
		fmt.Printf("You: %s\n", transcript)

		lower := strings.ToLower(transcript)
		if strings.Contains(lower, "quit") || strings.Contains(lower, "goodbye") {
			speak("Goodbye. Your session has been saved.")
			break
		}
		isDone := strings.Contains(lower, "done") || strings.Contains(lower, "finish") ||
			strings.Contains(lower, "that's all") || strings.Contains(lower, "thats all")

		sess.AddMessage("user", transcript)
		_ = sess.Save(path)

		// Score in background.
		type scoreResult struct {
			score int
			err   error
		}
		scoreCh := make(chan scoreResult, 1)
		go func(s *session.Session) {
			sc, e := scoreSession(scoreChatCfg, s)
			scoreCh <- scoreResult{sc, e}
		}(sess)

		// Filler if model takes >3s.
		fillerDone := make(chan struct{})
		fillerTimer := time.AfterFunc(3*time.Second, func() {
			select {
			case <-fillerDone:
			default:
				speak(fillerPhrases[rand.Intn(len(fillerPhrases))])
			}
		})

		if err := streamSpeak(voiceChatCfg, speak, fillerDone, transcript, sess, &history, path); err != nil {
			fillerTimer.Stop()
			return err
		}
		fillerTimer.Stop()
		select {
		case <-fillerDone:
		default:
			close(fillerDone)
		}

		sr := <-scoreCh
		if sr.err == nil {
			sess.Score = sr.score
			_ = sess.Save(path)
			fmt.Printf("[completeness: %d/100]\n", sr.score)
			if sr.score >= *threshold || isDone {
				speak("I think we have a solid picture now. Your brainstorm session is complete. Run gollama plan to generate your action plan.")
				sess.Status = session.StatusComplete
				_ = sess.Save(path)
				break
			}
		}
	}

	fmt.Printf("\nSession saved: %s\n", path)
	fmt.Printf("Generate a plan: gollama plan --session %s\n", path)
	return nil
}

func sendPipeCmd(w interface{ Write([]byte) (int, error) }, m pipeMsg) {
	b, _ := json.Marshal(m)
	b = append(b, '\n')
	w.Write(b)
}

// streamSpeak streams the model response sentence-by-sentence to TTS.
func streamSpeak(
	chatCfg chat.Config,
	speak func(string),
	fillerDone chan struct{},
	prompt string,
	sess *session.Session,
	history *[]chat.Msg,
	sessionPath string,
) error {
	prior := make([]chat.Msg, len(*history))
	copy(prior, *history)

	firstChunk := true
	var full strings.Builder
	var buf strings.Builder

	flush := func(force bool) {
		s := buf.String()
		for {
			idx := sentenceBoundary(s)
			if idx < 0 {
				break
			}
			sentence := strings.TrimSpace(s[:idx+1])
			buf.Reset()
			buf.WriteString(s[idx+1:])
			s = buf.String()
			if sentence != "" {
				speak(sentence)
			}
		}
		if force {
			if rem := strings.TrimSpace(buf.String()); rem != "" {
				speak(rem)
				buf.Reset()
			}
		}
	}

	chatErr := chat.ChatStream(chatCfg, prior, prompt, func(chunk string) error {
		if firstChunk && fillerDone != nil {
			select {
			case <-fillerDone:
			default:
				close(fillerDone)
			}
			firstChunk = false
		}
		full.WriteString(chunk)
		buf.WriteString(chunk)
		flush(false)
		return nil
	})
	flush(true)

	if chatErr != nil {
		return fmt.Errorf("model error: %w", chatErr)
	}

	reply := strings.TrimSpace(full.String())
	sess.AddMessage("assistant", reply)
	*history = append(*history,
		chat.Msg{Role: "user", Content: prompt},
		chat.Msg{Role: "assistant", Content: reply},
	)
	_ = sess.Save(sessionPath)
	return nil
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
