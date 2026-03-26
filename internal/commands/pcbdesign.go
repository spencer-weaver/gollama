package commands

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spencer-weaver/gollama/chat"
	"github.com/spencer-weaver/gollama/internal/config"
	"github.com/spencer-weaver/gollama/internal/session"
)

// PCBDesignHandler runs a PCB-specific brainstorm interview, then synthesizes the
// transcript into a structured design.md spec for the kicad model.
//
// Usage:
//
//	gollama pcb-design <design-name> [flags]
//
// Flags:
//
//	--threshold N     completeness score to end the interview (default 85)
//	--output path     path to write design.md (default: ~/.praxis/designs/<name>/electrical/design.md)
//	--session path    path to session file for resuming
func PCBDesignHandler(args []string) error {
	flags := flag.NewFlagSet("pcb-design", flag.ContinueOnError)
	threshold := flags.Int("threshold", 85, "completeness score required to end the interview (0-100)")
	outputPath := flags.String("output", "", "path to write design.md (default: ~/.praxis/designs/<name>/electrical/design.md)")
	sessionPath := flags.String("session", "", "path to resume an existing session")
	showThinking := flags.Bool("show-thinking", false, "show model reasoning (<think> blocks)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	remaining := flags.Args()
	if len(remaining) == 0 {
		return fmt.Errorf("no design name supplied\nusage: gollama pcb-design <name> [flags]")
	}
	name := remaining[0]

	cfg := config.GetGlobal()
	modelsDir := cfg.ModelsDir
	if modelsDir == "" {
		modelsDir = "models"
	}

	// Load model configs.
	interviewMC, err := config.LoadModelConfig(modelsDir, "pcb-brainstorm")
	if err != nil {
		return fmt.Errorf("pcb-brainstorm model config: %w\n(create models/pcb-brainstorm.json)", err)
	}
	scoreMC, err := config.LoadModelConfig(modelsDir, "score")
	if err != nil {
		return fmt.Errorf("score model config: %w", err)
	}
	synthMC, err := config.LoadModelConfig(modelsDir, "pcb-synth")
	if err != nil {
		return fmt.Errorf("pcb-synth model config: %w\n(create models/pcb-synth.json)", err)
	}

	interviewCfg := applyModelConfig(cfg, interviewMC)
	interviewCfg.ShowThinking = *showThinking
	scoreCfg := applyModelConfig(cfg, scoreMC)
	synthCfg := applyModelConfig(cfg, synthMC)

	// Resolve session path.
	sessPath := *sessionPath
	if sessPath == "" {
		sessDir := filepath.Join(config.GetGlobalRoot(), "sessions")
		sessPath = filepath.Join(sessDir, "pcb-"+name+".json")
	}

	// Resolve output path.
	out := *outputPath
	if out == "" {
		out = filepath.Join(os.Getenv("HOME"), ".praxis", "designs", name, "electrical", "design.md")
	}

	// Load or create session.
	var sess *session.Session
	if _, statErr := os.Stat(sessPath); statErr == nil {
		sess, err = session.Load(sessPath)
		if err != nil {
			return err
		}
		if sess.Status == session.StatusPlanned {
			fmt.Printf("Session already synthesized — design.md at: %s\n", out)
			return nil
		}
		fmt.Printf("Resuming PCB design session: %s\n\n", sessPath)
	} else {
		sess = session.New(name, *threshold)
	}

	history := msgsToHistory(sess.Messages)
	reader := bufio.NewReader(os.Stdin)

	printDivider()
	fmt.Printf("PCB Design  : %s\n", name)
	fmt.Printf("Threshold   : %d/100\n", *threshold)
	fmt.Printf("Output      : %s\n", out)
	fmt.Printf("Session     : %s\n", sessPath)
	printDivider()
	fmt.Println()

	// ── Initial question ────────────────────────────────────────────────────

	var currentQuestion string

	if len(history) == 0 {
		initialPrompt := fmt.Sprintf("I need to design a PCB for: %s", name)
		currentQuestion, err = chat.ChatWithHistory(interviewCfg, history, initialPrompt)
		if err != nil {
			return fmt.Errorf("interview error: %w", err)
		}
		sess.AddMessage("user", initialPrompt)
		sess.AddMessage("assistant", currentQuestion)
		history = append(history,
			chat.Msg{Role: "user", Content: initialPrompt},
			chat.Msg{Role: "assistant", Content: currentQuestion},
		)
		if err := sess.Save(sessPath); err != nil {
			return err
		}
	} else {
		currentQuestion = sess.LastAssistantMessage()
		if currentQuestion == "" {
			return fmt.Errorf("resumed session has no assistant messages")
		}
	}

	// ── Interview loop ──────────────────────────────────────────────────────

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
			fmt.Printf("\nSession saved: %s\n", sessPath)
			return nil
		}

		sess.AddMessage("user", userInput)
		_ = sess.Save(sessPath)

		score, scoreErr := scoreSession(scoreCfg, sess)
		if scoreErr != nil {
			fmt.Fprintf(os.Stderr, "[score error: %v]\n", scoreErr)
		} else {
			sess.Score = score
			fmt.Printf("[completeness: %d/100]\n", score)
			_ = sess.Save(sessPath)
			if score >= *threshold {
				printDivider()
				fmt.Printf("Interview complete — score %d/%d reached.\n", score, *threshold)
				printDivider()
				sess.Status = session.StatusComplete
				_ = sess.Save(sessPath)
				break
			}
		}

		nextQuestion, chatErr := chat.ChatWithHistory(interviewCfg, history, userInput)
		if chatErr != nil {
			return fmt.Errorf("interview error: %w", chatErr)
		}
		sess.AddMessage("assistant", nextQuestion)
		history = append(history,
			chat.Msg{Role: "user", Content: userInput},
			chat.Msg{Role: "assistant", Content: nextQuestion},
		)
		_ = sess.Save(sessPath)
		currentQuestion = nextQuestion
	}

	if sess.Status != session.StatusComplete {
		fmt.Printf("\nSession saved: %s\n", sessPath)
		return nil
	}

	// ── Synthesis step ──────────────────────────────────────────────────────

	fmt.Println()
	printDivider()
	fmt.Println("Synthesizing design.md...")
	printDivider()

	synthPrompt := fmt.Sprintf(
		"Here is the PCB design interview transcript. Produce the design.md for a board named %q.\n\nTranscript:\n%s",
		name, sess.Transcript(),
	)
	designContent, err := chat.Chat(synthCfg, synthPrompt)
	if err != nil {
		return fmt.Errorf("synthesis error: %w", err)
	}

	// Write design.md.
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	if err := os.WriteFile(out, []byte(designContent+"\n"), 0o644); err != nil {
		return fmt.Errorf("write design.md: %w", err)
	}

	sess.Status = session.StatusPlanned
	_ = sess.Save(sessPath)

	fmt.Printf("\ndesign.md written to: %s\n", out)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Review design.md and fill in any TBD sections")
	fmt.Println("  2. Run part-research for any UNKNOWN components")
	fmt.Printf("  3. cd %s && gollama chat -m kicad \"generate schematic per design.md\"\n",
		filepath.Dir(out))

	return nil
}
