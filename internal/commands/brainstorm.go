package commands

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spencer-weaver/gollama/chat"
	"github.com/spencer-weaver/gollama/internal/config"
	"github.com/spencer-weaver/gollama/internal/session"
)

// BrainstormHandler runs an interactive brainstorm session against a local Ollama model.
// The model acts as an interrogator, asking focused questions to gather information
// about a topic or goal. After each user response a lightweight score model evaluates
// how complete the gathered information is. Once the score reaches the threshold the
// session is marked complete and the user is prompted to generate a plan.
//
// Flags:
//
//	--threshold N   completeness score required to end brainstorm (default 80)
//	--session path  path to session file (default: sessions/<slug>.json)
//	--sessions dir  directory for session files (default: sessions)
//	--no-plan       skip the automatic prompt to run plan after brainstorm ends
func BrainstormHandler(args []string) error {
	flags := flag.NewFlagSet("brainstorm", flag.ContinueOnError)
	threshold := flags.Int("threshold", 80, "completeness score (0-100) required to end brainstorm")
	sessionPath := flags.String("session", "", "path to session file")
	defaultSessionsDir := filepath.Join(config.GetGlobalRoot(), "sessions")
	sessionsDir := flags.String("sessions", defaultSessionsDir, "directory for session files")
	noPlan := flags.Bool("no-plan", false, "skip automatic plan prompt after brainstorm ends")
	showThinking := flags.Bool("show-thinking", false, "show model reasoning (<think> blocks) in output")
	if err := flags.Parse(args); err != nil {
		return err
	}

	remaining := flags.Args()
	if len(remaining) == 0 {
		return fmt.Errorf("no topic supplied\nusage: gollama brainstorm [flags] <topic>")
	}
	topic := strings.Join(remaining, " ")

	cfg := config.GetGlobal()
	modelsDir := cfg.ModelsDir
	if modelsDir == "" {
		modelsDir = "models"
	}

	// Load model configs.
	brainstormMC, err := config.LoadModelConfig(modelsDir, "brainstorm")
	if err != nil {
		return fmt.Errorf("brainstorm model config: %w\n(create models/brainstorm.json)", err)
	}
	scoreMC, err := config.LoadModelConfig(modelsDir, "score")
	if err != nil {
		return fmt.Errorf("score model config: %w\n(create models/score.json)", err)
	}

	brainstormChatCfg := applyModelConfig(cfg, brainstormMC)
	brainstormChatCfg.ShowThinking = *showThinking
	scoreChatCfg := applyModelConfig(cfg, scoreMC)
	// Score model never shows thinking — it only needs to emit a JSON score.

	// Resolve session path.
	path := *sessionPath
	if path == "" {
		path = session.DefaultPath(*sessionsDir, topic)
	}

	// Load existing session or create a new one.
	var sess *session.Session
	if _, statErr := os.Stat(path); statErr == nil {
		sess, err = session.Load(path)
		if err != nil {
			return err
		}
		if sess.Status == session.StatusPlanned {
			return fmt.Errorf("session already has a plan — run: gollama plan --session %s", path)
		}
		fmt.Printf("Resuming session: %s\n\n", path)
	} else {
		sess = session.New(topic, *threshold)
	}

	// Build chat history from existing session messages.
	history := msgsToHistory(sess.Messages)

	reader := bufio.NewReader(os.Stdin)

	printDivider()
	fmt.Printf("Topic     : %s\n", topic)
	fmt.Printf("Threshold : %d/100\n", *threshold)
	fmt.Printf("Session   : %s\n", path)
	printDivider()
	fmt.Println()

	// ── First turn ────────────────────────────────────────────────────────────
	// If this is a new session, prime the brainstorm model with the topic and
	// get its first question. For a resumed session, use the last assistant msg.

	var currentQuestion string

	if len(history) == 0 {
		initialPrompt := fmt.Sprintf("I want to brainstorm about the following topic: %s", topic)
		currentQuestion, err = chat.ChatWithHistory(brainstormChatCfg, history, initialPrompt)
		if err != nil {
			return fmt.Errorf("brainstorm error: %w", err)
		}
		sess.AddMessage("user", initialPrompt)
		sess.AddMessage("assistant", currentQuestion)
		history = append(history,
			chat.Msg{Role: "user", Content: initialPrompt},
			chat.Msg{Role: "assistant", Content: currentQuestion},
		)
		if err := sess.Save(path); err != nil {
			return err
		}
	} else {
		currentQuestion = sess.LastAssistantMessage()
		if currentQuestion == "" {
			return fmt.Errorf("resumed session has no assistant messages — session may be corrupt")
		}
	}

	// ── Main loop ─────────────────────────────────────────────────────────────
	for {
		fmt.Printf("\nInterviewer: %s\n\n", currentQuestion)
		fmt.Print("You: ")

		userInput, readErr := reader.ReadString('\n')
		if readErr != nil {
			break
		}
		userInput = strings.TrimSpace(userInput)
		if userInput == "" {
			continue
		}
		if userInput == "quit" || userInput == "exit" {
			fmt.Printf("\nSession saved: %s\n", path)
			return nil
		}

		sess.AddMessage("user", userInput)
		if saveErr := sess.Save(path); saveErr != nil {
			fmt.Fprintf(os.Stderr, "save error: %v\n", saveErr)
		}

		// Score the session after the user response.
		score, scoreErr := scoreSession(scoreChatCfg, sess)
		if scoreErr != nil {
			fmt.Fprintf(os.Stderr, "[score error: %v]\n", scoreErr)
		} else {
			sess.Score = score
			fmt.Printf("[completeness: %d/100]\n", score)
			if saveErr := sess.Save(path); saveErr != nil {
				fmt.Fprintf(os.Stderr, "save error: %v\n", saveErr)
			}
			if score >= *threshold {
				printDivider()
				fmt.Printf("Brainstorm complete — score %d/%d reached.\n", score, *threshold)
				printDivider()
				sess.Status = session.StatusComplete
				_ = sess.Save(path)
				break
			}
		}

		// Get the next question from the brainstorm model.
		nextQuestion, chatErr := chat.ChatWithHistory(brainstormChatCfg, history, userInput)
		if chatErr != nil {
			return fmt.Errorf("brainstorm error: %w", chatErr)
		}

		sess.AddMessage("assistant", nextQuestion)
		history = append(history,
			chat.Msg{Role: "user", Content: userInput},
			chat.Msg{Role: "assistant", Content: nextQuestion},
		)
		if saveErr := sess.Save(path); saveErr != nil {
			fmt.Fprintf(os.Stderr, "save error: %v\n", saveErr)
		}

		currentQuestion = nextQuestion
	}

	fmt.Printf("\nSession saved: %s\n", path)

	if !*noPlan && sess.Status == session.StatusComplete {
		fmt.Println("\nRun the following to generate a plan:")
		fmt.Printf("  gollama plan --session %s\n", path)
	}

	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// applyModelConfig builds a chat.Config from the global config, overriding
// fields from a ModelConfig where set.
func applyModelConfig(cfg *config.GollamaConfig, mc *config.ModelConfig) chat.Config {
	c := chat.Config{
		Host:        cfg.Host,
		Port:        cfg.Port,
		Model:       cfg.Model,
		System:      cfg.System,
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
	}
	if mc.Model != "" {
		c.Model = mc.Model
	}
	if mc.System != "" {
		c.System = mc.System
	}
	if mc.Temperature != 0 {
		c.Temperature = mc.Temperature
	}
	if mc.MaxTokens != 0 {
		c.MaxTokens = mc.MaxTokens
	}
	return c
}

// msgsToHistory converts session messages to chat history slices.
func msgsToHistory(msgs []session.Message) []chat.Msg {
	history := make([]chat.Msg, len(msgs))
	for i, m := range msgs {
		history[i] = chat.Msg{Role: m.Role, Content: m.Content}
	}
	return history
}

// scoreSession asks the score model to evaluate how complete the brainstorm
// transcript is. Returns a 0-100 integer score.
func scoreSession(scoreCfg chat.Config, sess *session.Session) (int, error) {
	if len(sess.Messages) == 0 {
		return 0, nil
	}

	prompt := fmt.Sprintf(
		"Evaluate the completeness of this brainstorm transcript. "+
			"Return ONLY a JSON object in the format {\"score\": N} where N is 0-100. "+
			"100 means all information needed to create a detailed action plan is present. "+
			"Do not include any other text.\n\nTranscript:\n%s",
		sess.Transcript(),
	)

	reply, err := chat.Chat(scoreCfg, prompt)
	if err != nil {
		return 0, err
	}

	return parseScore(reply)
}

// reScore extracts the first integer that looks like a score from model output.
var reScore = regexp.MustCompile(`\b([0-9]{1,3})\b`)

// parseScore tries JSON first, then falls back to the first integer in the reply.
func parseScore(reply string) (int, error) {
	// Try structured JSON {"score": N}
	var result struct {
		Score int `json:"score"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(reply)), &result); err == nil {
		return clamp(result.Score, 0, 100), nil
	}

	// Fallback: extract the first number from the reply.
	m := reScore.FindString(reply)
	if m == "" {
		return 0, fmt.Errorf("could not parse score from: %q", reply)
	}
	n, err := strconv.Atoi(m)
	if err != nil {
		return 0, err
	}
	return clamp(n, 0, 100), nil
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func printDivider() {
	fmt.Println("─────────────────────────────────────────")
}
