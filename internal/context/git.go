package context

import (
	"fmt"
	"os/exec"
)

// IsGitRepo returns true if the project directory is inside a git repository.
func IsGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// CommitContextFiles stages README.md and .gollama/context.md and commits
// with the given message. Returns an error if git is not available or commit fails.
func CommitContextFiles(dir, message string) error {
	if err := runGit(dir, "add", "README.md", ".gollama/context.md"); err != nil {
		return err
	}
	return runGit(dir, "commit", "-m", message)
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", args[0], err, string(out))
	}
	return nil
}
