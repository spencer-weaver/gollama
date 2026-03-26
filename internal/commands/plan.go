package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spencer-weaver/gollama/chat"
	"github.com/spencer-weaver/gollama/internal/config"
	"github.com/spencer-weaver/gollama/internal/session"
)

// PlanHandler reads a completed brainstorm session and generates a concrete,
// step-by-step action plan. The plan is written back into the session file
// and printed to stdout.
//
// Flags:
//
//	--session path   path to the session file to plan from (required)
//	--sessions dir   directory to search for session files (default: sessions)
//	--topic name     topic slug to derive session path (alternative to --session)
func PlanHandler(args []string) error {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	sessionPath := flags.String("session", "", "path to session file")
	defaultSessionsDir := filepath.Join(config.GetGlobalRoot(), "sessions")
	sessionsDir := flags.String("sessions", defaultSessionsDir, "directory for session files")
	topicSlug := flags.String("topic", "", "topic slug to derive session path (e.g. my-project)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg := config.GetGlobal()
	modelsDir := cfg.ModelsDir
	if modelsDir == "" {
		modelsDir = "models"
	}

	// Load plan model config.
	planMC, err := config.LoadModelConfig(modelsDir, "plan")
	if err != nil {
		return fmt.Errorf("plan model config: %w\n(create models/plan.json)", err)
	}
	planChatCfg := applyModelConfig(cfg, planMC)

	// Resolve session path.
	path := *sessionPath
	switch {
	case path != "":
		// use as-is
	case *topicSlug != "":
		path = session.DefaultPath(*sessionsDir, *topicSlug)
	case len(flags.Args()) > 0:
		// Accept a bare positional argument as the session path.
		path = flags.Args()[0]
	default:
		// List available sessions to help the user.
		return listSessions(*sessionsDir)
	}

	// Load session.
	sess, err := session.Load(path)
	if err != nil {
		return err
	}

	if len(sess.Messages) == 0 {
		return fmt.Errorf("session %s has no messages — run brainstorm first", path)
	}

	if sess.Status == session.StatusPlanned && sess.Plan != "" {
		fmt.Printf("Plan already exists for this session (score %d/100).\n", sess.Score)
		fmt.Println("Regenerate? [y/N]: ")
		var answer string
		fmt.Scanln(&answer)
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			fmt.Println(sess.Plan)
			return nil
		}
	}

	printDivider()
	fmt.Printf("Generating plan for: %s\n", sess.Topic)
	fmt.Printf("Model: %s\n", planChatCfg.Model)
	printDivider()
	fmt.Println()

	// Build the planning prompt from the transcript.
	planPrompt := buildPlanPrompt(sess)

	plan, err := chat.Chat(planChatCfg, planPrompt)
	if err != nil {
		return fmt.Errorf("plan model error: %w", err)
	}

	// Persist the plan into the session.
	sess.Plan = plan
	sess.Status = session.StatusPlanned
	if saveErr := sess.Save(path); saveErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save session: %v\n", saveErr)
	}

	printDivider()
	fmt.Println(plan)
	printDivider()
	fmt.Printf("\nPlan saved to session: %s\n", path)

	return nil
}

// buildPlanPrompt constructs the prompt sent to the plan model.
func buildPlanPrompt(sess *session.Session) string {
	var sb strings.Builder
	sb.WriteString("Based on the following brainstorm session, create a detailed, concrete, step-by-step action plan.\n\n")
	sb.WriteString("Requirements for the plan:\n")
	sb.WriteString("- Break the work into clear phases with numbered steps\n")
	sb.WriteString("- Each step should be specific and actionable\n")
	sb.WriteString("- Include success criteria for each phase\n")
	sb.WriteString("- Flag any risks, dependencies, or open questions\n")
	sb.WriteString("- Prioritize the most impactful steps first\n\n")
	sb.WriteString("Brainstorm transcript:\n")
	sb.WriteString("---\n")
	sb.WriteString(sess.Transcript())
	sb.WriteString("\n---\n\n")
	sb.WriteString("Now generate the action plan:")
	return sb.String()
}

// listSessions prints the available sessions in the sessions directory and
// returns an error so the caller knows no session was selected.
func listSessions(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no sessions directory found at %q — run brainstorm first", dir)
		}
		return err
	}

	var sessions []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			sessions = append(sessions, e.Name())
		}
	}

	if len(sessions) == 0 {
		return fmt.Errorf("no sessions found in %q — run brainstorm first", dir)
	}

	fmt.Printf("Available sessions in %s:\n\n", dir)
	for _, s := range sessions {
		fmt.Printf("  %s/%s\n", dir, s)
	}
	fmt.Println("\nUsage: gollama plan --session <path>")
	return fmt.Errorf("no session specified")
}
