package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This exercises ancestry, not just successive commits on a single branch.
func TestCLIHistoryBoundaries(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "pushscan")
	command(t, ".", "go", "build", "-o", binary, ".")
	scan := func(t *testing.T, dir, base, head, evidence string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, base, head)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PUSHSCAN_LITERALS=")
		out, err := cmd.CombinedOutput()
		requireExit(t, err, 1)
		if !strings.Contains(string(out), evidence) {
			t.Fatalf("missing rejection evidence %s: %s", evidence, out)
		}
	}
	t.Run("merged-side-history", func(t *testing.T) {
		dir, base := fixture(t)
		branch := strings.TrimSpace(string(command(t, dir, "git", "symbolic-ref", "--short", "HEAD")))
		command(t, dir, "git", "checkout", "-qb", "side")
		commitFile(t, dir, "add", strings.Repeat("c3", 16)+"\n")
		added := strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
		commitFile(t, dir, "remove", "safe\n")
		command(t, dir, "git", "checkout", "-q", branch)
		command(t, dir, "git", "merge", "--no-ff", "-qm", "merge", "side")
		scan(t, dir, base, "HEAD", "commit="+added)
	})
	t.Run("net-only-backwards-range", func(t *testing.T) {
		dir, _ := fixture(t)
		commitFile(t, dir, "add", strings.Repeat("d4", 16)+"\n")
		head := strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
		commitFile(t, dir, "remove", "safe\n")
		base := strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
		// Head adds no reachable commits; only the net diff can catch this reintroduction.
		scan(t, dir, base, head, "commit=net-diff")
	})
	t.Run("merge-resolution", func(t *testing.T) {
		dir, base := fixture(t)
		branch := strings.TrimSpace(string(command(t, dir, "git", "symbolic-ref", "--short", "HEAD")))
		command(t, dir, "git", "checkout", "-qb", "side")
		commitFile(t, dir, "side", "side-safe\n")
		command(t, dir, "git", "checkout", "-q", branch)
		commitFile(t, dir, "main", "main-safe\n")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		merge := exec.CommandContext(ctx, "git", "merge", "--no-ff", "--no-commit", "side")
		merge.Dir = dir
		if err := merge.Run(); err == nil {
			t.Fatal("expected fixture merge conflict")
		}
		commitFile(t, dir, "resolution", strings.Repeat("e5", 16)+"\n")
		resolved := strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
		commitFile(t, dir, "remove", "safe\n")
		scan(t, dir, base, "HEAD", "commit="+resolved)
	})
}
