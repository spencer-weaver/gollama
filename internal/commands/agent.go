package commands

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spencer-weaver/gollama/chat"
	"github.com/spencer-weaver/gollama/internal/config"
	"github.com/spencer-weaver/gollama/internal/tasks"
	"github.com/spencer-weaver/gollama/internal/tools"
	"github.com/spencer-weaver/gollama/internal/voice"
)

const (
	agentMaxHistory     = 40  // 20 turns = 40 messages
	agentMaxToolIter    = 5
	agentUpdateTruncate = 300 // chars injected per background result
)

// agentHandler is the persistent voice conversation loop.
type agentHandler struct {
	chatCfg      chat.Config
	tts          *voice.TTS
	taskMgr      *tasks.Manager
	reg          *tools.Registry
	allowedTools []string
	history      []chat.Msg
	projectRoot  string

	// STT subprocess
	sttCmd   *exec.Cmd
	sttPipeIn io.WriteCloser // stdin of voice_pipe.py — used for mute/unmute/quit
	events    chan pipeMsg   // reuses pipeMsg from voice.go (same package)

	// Conversation state — protected by stateMu
	stateMu    sync.Mutex
	idle       bool
	lastTurnAt time.Time

	// Config
	audioDevice    string
	playbackDevice string
	sttModel       string
	debug          bool
	proactiveDelay time.Duration
}

// AgentRun is the entry point for `gollama agent`.
func AgentRun(args []string) error {
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	audioDevice   := flags.String("audio-device", "plughw:2,0", "ALSA capture device (see: arecord -l)")
	playbackDevice := flags.String("playback-device", "", "ALSA playback device (empty=default)")
	sttModel      := flags.String("stt-model", "base.en", "STT model: tiny.en, base.en, small.en")
	proactiveSecs := flags.Int("proactive-delay", 5, "seconds of silence before proactively speaking completed tasks")
	debugFlag     := flags.Bool("debug", false, "verbose diagnostics")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg  := config.GetGlobal()
	root := config.GetGlobalRoot()

	mc, err := config.LoadModelConfig(cfg.ModelsDir, "agent")
	if err != nil {
		return fmt.Errorf("agent model config: %w", err)
	}

	tts, err := voice.DefaultTTS(root)
	if err != nil {
		return err
	}
	tts.PlaybackDevice = *playbackDevice
	tts.Debug = *debugFlag

	taskMgr := tasks.NewManager()

	allowed := []string{"run_command", "spawn_background", "ask_claude"}
	reg := tools.NewRegistry()
	reg.Register(tools.NewRunCommandTool())
	reg.Register(tools.NewSpawnBackgroundTool(taskMgr))
	reg.Register(tools.NewAskClaudeTool(taskMgr))

	chatCfg := applyModelConfig(cfg, mc)
	chatCfg.System = reg.ToolGuide(allowed) + mc.System

	h := &agentHandler{
		chatCfg:        chatCfg,
		tts:            tts,
		taskMgr:        taskMgr,
		reg:            reg,
		allowedTools:   allowed,
		projectRoot:    root,
		audioDevice:    *audioDevice,
		playbackDevice: *playbackDevice,
		sttModel:       *sttModel,
		debug:          *debugFlag,
		proactiveDelay: time.Duration(*proactiveSecs) * time.Second,
		idle:           true,
		lastTurnAt:     time.Now(),
	}

	return h.run()
}

// run is the main conversation loop.
func (h *agentHandler) run() error {
	if err := h.startSTT(); err != nil {
		return err
	}
	defer h.stopSTT()

	h.speakDirect("Praxis ready.")

	go h.proactiveLoop()

	for {
		h.setIdle(true)
		if h.debug {
			fmt.Fprintln(os.Stderr, "[agent] listening...")
		}

		ev, ok := <-h.events
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

		h.setIdle(false)
		h.setLastTurn(time.Now())
		h.muteSTT()

		exit, err := h.runTurn(transcript)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[agent error: %v]\n", err)
			h.speakDirect("Sorry, I ran into a problem.")
		}

		h.unmuteSTT()

		if exit {
			h.speakDirect("Goodbye.")
			return nil
		}
	}
}

// runTurn handles one user utterance: drains background updates, builds the
// model prompt, streams the response sentence-by-sentence, and runs any tool
// calls before looping back.
func (h *agentHandler) runTurn(transcript string) (exit bool, err error) {
	updates := h.taskMgr.DrainUpdates()
	prompt := agentBuildPrompt(transcript, updates)

	h.history = append(h.history, chat.Msg{Role: "user", Content: prompt})

	currentPrompt := prompt
	var fullReply strings.Builder

	for i := 0; i < agentMaxToolIter; i++ {
		// Filler: speak one phrase after 3s if the model hasn't responded yet.
		fillerStop := make(chan struct{})
		go func() {
			select {
			case <-time.After(3 * time.Second):
				select {
				case <-fillerStop:
				default:
					h.speakDirect(fillerPhrases[rand.Intn(len(fillerPhrases))])
				}
			case <-fillerStop:
			}
		}()
		stopFiller := sync.OnceFunc(func() { close(fillerStop) })

		// Stream the model response, speaking sentences and suppressing tool blocks.
		var full strings.Builder
		var buf strings.Builder
		inToolBlock := false

		streamErr := chat.ChatStream(h.chatCfg, h.history, currentPrompt, func(chunk string) error {
			stopFiller() // cancel filler on first chunk

			full.WriteString(chunk)

			if inToolBlock {
				return nil
			}

			// Detect <tool_calls> block — stop speaking once it starts.
			if strings.Contains(full.String(), "<tool_calls>") {
				inToolBlock = true
				pre := full.String()
				if idx := strings.Index(pre, "<tool_calls>"); idx > 0 {
					buf.Reset()
					buf.WriteString(pre[:idx])
					h.flushSentences(&buf, true)
				}
				return nil
			}

			buf.WriteString(chunk)
			h.flushSentences(&buf, false)
			return nil
		})

		stopFiller()

		if !inToolBlock {
			h.flushSentences(&buf, true)
		}

		if streamErr != nil {
			return false, fmt.Errorf("model error: %w", streamErr)
		}

		response := strings.TrimSpace(full.String())

		// Exit sentinel — checked before TTS can speak anything further.
		if strings.Contains(response, "AGENT_EXIT") {
			return true, nil
		}

		calls, cleaned, hasCalls := tools.ParseToolCalls(response)

		if cleaned != "" {
			fullReply.WriteString(cleaned)
		}

		if !hasCalls {
			h.history = append(h.history, chat.Msg{Role: "assistant", Content: response})
			break
		}

		toolResults := h.executeTools(calls)

		h.history = append(h.history,
			chat.Msg{Role: "assistant", Content: response},
			chat.Msg{Role: "user", Content: toolResults},
		)
		currentPrompt = toolResults
	}

	h.trimHistory()
	return false, nil
}

// executeTools runs each call. Non-blocking tools (spawn_background, ask_claude)
// get an immediate verbal confirmation. Blocking tools get a "one moment" filler.
func (h *agentHandler) executeTools(calls []tools.ToolCall) string {
	var sb strings.Builder
	sb.WriteString("Tool results:\n")
	for i, call := range calls {
		switch call.Tool {
		case "spawn_background", "ask_claude":
			result := h.reg.Execute(call)
			sb.WriteString(fmt.Sprintf("\n[%d] %s:\n%s\n", i+1, call.Tool, result))
			h.speakDirect("I'll work on that in the background.")
		default:
			h.speakDirect("One moment.")
			result := h.reg.Execute(call)
			sb.WriteString(fmt.Sprintf("\n[%d] %s:\n%s\n", i+1, call.Tool, result))
		}
	}
	return sb.String()
}

// proactiveLoop ticks every second and speaks completed task summaries when
// the conversation has been idle longer than proactiveDelay.
func (h *agentHandler) proactiveLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if !h.taskMgr.HasPending() {
			continue
		}
		h.stateMu.Lock()
		isIdle := h.idle
		since := time.Since(h.lastTurnAt)
		h.stateMu.Unlock()

		if !isIdle || since < h.proactiveDelay {
			continue
		}

		// Claim ownership to prevent race with main loop.
		h.stateMu.Lock()
		h.idle = false
		h.stateMu.Unlock()

		updates := h.taskMgr.DrainUpdates()
		if len(updates) == 0 {
			h.setIdle(true)
			continue
		}

		h.muteSTT()
		for _, u := range updates {
			h.speakDirect(agentUpdateSpeech(u))
		}
		h.unmuteSTT()

		h.stateMu.Lock()
		h.idle = true
		h.lastTurnAt = time.Now()
		h.stateMu.Unlock()
	}
}

// startSTT spawns voice_pipe.py and waits for the "ready" event.
func (h *agentHandler) startSTT() error {
	pipeScript := filepath.Join(h.projectRoot, "scripts", "voice_pipe.py")
	pythonBin  := filepath.Join(h.projectRoot, ".venv", "bin", "python3")
	pipeArgs   := []string{pipeScript, "--model", h.sttModel, "--device", h.audioDevice}
	if h.debug {
		pipeArgs = append(pipeArgs, "--debug")
	}

	h.sttCmd = exec.Command(pythonBin, pipeArgs...)
	h.sttCmd.Stderr = os.Stderr

	pipeIn, err := h.sttCmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stt stdin pipe: %w", err)
	}
	pipeOut, err := h.sttCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stt stdout pipe: %w", err)
	}
	if err := h.sttCmd.Start(); err != nil {
		return fmt.Errorf("start voice_pipe.py: %w\n(ensure .venv is set up)", err)
	}

	h.sttPipeIn = pipeIn

	h.events = make(chan pipeMsg, 16)
	go func() {
		scanner := bufio.NewScanner(pipeOut)
		for scanner.Scan() {
			var m pipeMsg
			if json.Unmarshal([]byte(scanner.Text()), &m) == nil {
				h.events <- m
			}
		}
		close(h.events)
	}()

	fmt.Println("[initialising speech recognition...]")
	for ev := range h.events {
		if ev.Event == "error" {
			return fmt.Errorf("stt: %s", ev.Msg)
		}
		if ev.Event == "ready" {
			if h.debug {
				fmt.Fprintln(os.Stderr, "[agent] STT ready")
			}
			return nil
		}
	}
	return fmt.Errorf("STT exited before ready")
}

func (h *agentHandler) stopSTT() {
	sendPipeCmd(h.sttPipeIn, pipeMsg{Cmd: "quit"})
	h.sttCmd.Wait()
}

func (h *agentHandler) muteSTT()   { sendPipeCmd(h.sttPipeIn, pipeMsg{Cmd: "mute"}) }
func (h *agentHandler) unmuteSTT() { sendPipeCmd(h.sttPipeIn, pipeMsg{Cmd: "unmute"}) }

// speakDirect speaks text without toggling mute — call only when STT is already muted.
func (h *agentHandler) speakDirect(text string) {
	if text == "" {
		return
	}
	fmt.Printf("Praxis: %s\n", text)
	if err := h.tts.Speak(text); err != nil {
		fmt.Fprintf(os.Stderr, "[tts error: %v]\n", err)
	}
}

// flushSentences speaks complete sentences from buf.
// On force=true it also flushes remainder, stripping any partial tool block.
func (h *agentHandler) flushSentences(buf *strings.Builder, force bool) {
	s := buf.String()
	for {
		idx := sentenceBoundary(s) // defined in voice.go, same package
		if idx < 0 {
			break
		}
		sentence := strings.TrimSpace(s[:idx+1])
		buf.Reset()
		buf.WriteString(s[idx+1:])
		s = buf.String()
		if sentence != "" {
			h.speakDirect(sentence)
		}
	}
	if force {
		rem := strings.TrimSpace(buf.String())
		if idx := strings.Index(rem, "<tool_calls>"); idx >= 0 {
			rem = strings.TrimSpace(rem[:idx])
		}
		if rem != "" {
			h.speakDirect(rem)
		}
		buf.Reset()
	}
}

func (h *agentHandler) trimHistory() {
	if len(h.history) > agentMaxHistory {
		h.history = h.history[len(h.history)-agentMaxHistory:]
	}
}

func (h *agentHandler) setIdle(v bool) {
	h.stateMu.Lock()
	h.idle = v
	h.stateMu.Unlock()
}

func (h *agentHandler) setLastTurn(t time.Time) {
	h.stateMu.Lock()
	h.lastTurnAt = t
	h.stateMu.Unlock()
}

// agentBuildPrompt prepends background task results to the user utterance so
// the model can weave them naturally into its spoken response.
func agentBuildPrompt(transcript string, updates []tasks.TaskResult) string {
	if len(updates) == 0 {
		return transcript
	}
	var sb strings.Builder
	sb.WriteString("[Background updates]\n")
	for _, u := range updates {
		out := u.Output
		if len(out) > agentUpdateTruncate {
			out = out[:agentUpdateTruncate] + " [...]"
		}
		if u.Err != nil {
			sb.WriteString(fmt.Sprintf("- %s: FAILED — %v\n", u.Label, u.Err))
		} else {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", u.Label, strings.TrimSpace(out)))
		}
	}
	sb.WriteString("\n[User says]\n")
	sb.WriteString(transcript)
	return sb.String()
}

// agentUpdateSpeech builds a short proactive notification for a completed task.
func agentUpdateSpeech(u tasks.TaskResult) string {
	elapsed := u.Elapsed.Round(time.Second)
	if u.Err != nil {
		return fmt.Sprintf("By the way, %s failed.", u.Label)
	}
	return fmt.Sprintf("Just a heads up — %s finished in %s.", u.Label, elapsed)
}
