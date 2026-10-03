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
func TestCLIHistoricalAllowedAndRemovedLiteral(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "pushscan")
	command(t, ".", "go", "build", "-o", binary, ".")
	dir, base := fixture(t)
	literal := "private-" + "publication-exception"
	allowed := strings.Join([]string{"192", "0", "2", "17"}, ".")
	prohibitedIPv6 := "2002" + strings.Repeat(":", 2) + "1"
	commitFile(t, dir, "prohibited-ipv6", prohibitedIPv6+"\n")
	prohibitedCommit := strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
	allowedIPv6 := strings.Repeat(":", 2) + "1"
	commitFile(t, dir, "historical-add", allowed+"\n"+allowedIPv6+"\n"+literal+"\n")
	added := strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
	commitFile(t, dir, "historical-remove", "safe\n")
	head := strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
	before := strings.TrimSpace(string(command(t, dir, "git", "rev-list", "--reverse", base+".."+head)))
	literals := filepath.Join(t.TempDir(), "literals")
	if err := os.WriteFile(literals, []byte(literal+"\n"), 0600); err != nil {
		t.Fatal("cannot create fixture literal list")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, base, head)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PUSHSCAN_LITERALS="+literals)
	out, err := cmd.CombinedOutput()
	requireExit(t, err, 1)
	text := string(out)
	for _, evidence := range []string{
		`file="fixture.txt" commit=` + added + " class=literal",
		`file="fixture.txt" commit=` + added + " class=ipv4 allowed=true",
		`file="fixture.txt" commit=` + added + " class=ipv6 allowed=true",
		`file="fixture.txt" commit=` + prohibitedCommit + " class=ipv6\n",
	} {
		if !strings.Contains(text, evidence) {
			t.Fatalf("missing historical publication evidence %q: %s", evidence, text)
		}
	}
	if strings.Contains(text, "commit="+prohibitedCommit+" class=ipv6 allowed=true") {
		t.Fatalf("prohibited complete IPv6 address reported as allowed: %s", text)
	}
	// The same complete address must not gain an exception in the net-diff report.
	cmd = exec.CommandContext(ctx, binary, base, prohibitedCommit)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PUSHSCAN_LITERALS="+literals)
	netOut, netErr := cmd.CombinedOutput()
	requireExit(t, netErr, 1)
	if !strings.Contains(string(netOut), "commit=net-diff class=ipv6\n") || strings.Contains(string(netOut), "allowed=true") {
		t.Fatalf("inaccurate prohibited IPv6 net-diff report: %s", netOut)
	}
	if strings.Contains(text, prohibitedIPv6) || strings.Contains(text, allowedIPv6) {
		t.Fatal("scanner leaked an IPv6 literal")
	}
	if strings.Contains(text, literal) || strings.Contains(text, head) {
		t.Fatal("scanner leaked the literal or misattributed the removal commit")
	}
	after := strings.TrimSpace(string(command(t, dir, "git", "rev-list", "--reverse", base+".."+head)))
	if after != before {
		t.Fatal("scanner rewrote candidate history")
	}
}

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
	for _, tc := range []struct{ name, text string }{
		{"hex", strings.Repeat("d4", 16)},
		{"colon-prefix", "endpoint:" + "2606" + ":" + "4700" + strings.Repeat(":", 2) + "1111"},
		{"hex-suffix", "2606" + ":" + "4700" + strings.Repeat(":", 2) + "1111" + ".dead"},
	} {
		t.Run("net-only-backwards-range/"+tc.name, func(t *testing.T) {
			dir, _ := fixture(t)
			commitFile(t, dir, "add", tc.text+"\n")
			head := strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
			commitFile(t, dir, "remove", "safe\n")
			base := strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
			// Head adds no reachable commits; only the net diff can catch this reintroduction.
			scan(t, dir, base, head, "commit=net-diff")
		})
	}
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
