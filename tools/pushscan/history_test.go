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
	prohibitedCases := []struct {
		name, text, commit string
		standaloneIPv4     bool
	}{
		{name: "prohibited-ipv6", text: prohibitedIPv6},
		{name: "embedded-ipv4", text: "2002" + strings.Repeat(":", 2) + allowed},
		{name: "embedded-and-standalone-ipv4", text: "2002" + strings.Repeat(":", 2) + allowed + " " + allowed, standaloneIPv4: true},
	}
	for i := range prohibitedCases {
		commitFile(t, dir, prohibitedCases[i].name, prohibitedCases[i].text+"\n")
		prohibitedCases[i].commit = strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
	}
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
	} {
		if !strings.Contains(text, evidence) {
			t.Fatalf("missing historical publication evidence %q: %s", evidence, text)
		}
	}
	previous := base
	for _, tc := range prohibitedCases {
		// Isolate each addition for net-diff proof as well as full history above.
		cmd = exec.CommandContext(ctx, binary, previous, tc.commit)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PUSHSCAN_LITERALS="+literals)
		netOut, netErr := cmd.CombinedOutput()
		requireExit(t, netErr, 1)
		for _, report := range []struct{ output, commit string }{
			{text, tc.commit}, {string(netOut), "net-diff"},
		} {
			prefix := `file="fixture.txt" commit=` + report.commit
			if !strings.Contains(report.output, prefix+" class=ipv6\n") {
				t.Fatalf("missing prohibited IPv6 evidence (%s): %s", tc.name, report.output)
			}
			if strings.Contains(report.output, prefix+" class=ipv6 allowed=true") {
				t.Fatalf("prohibited complete IPv6 address reported as allowed (%s): %s", tc.name, report.output)
			}
			if strings.Contains(report.output, prefix+" class=ipv4 allowed=true") != tc.standaloneIPv4 {
				t.Fatalf("inaccurate standalone IPv4 exception report (%s): %s", tc.name, report.output)
			}
			if strings.Contains(report.output, tc.text) {
				t.Fatal("scanner leaked a prohibited address")
			}
		}
		previous = tc.commit
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
	for _, address := range []string{
		"2001" + ":db8" + strings.Repeat(":", 2) + "1",
		strings.Repeat(":", 2) + "ffff:" + allowed,
		strings.Repeat(":", 2) + "ffff:" + strings.Join([]string{"127", "0", "0", "1"}, "."),
	} {
		// Whole documentation and mapped addresses remain IPv6 exceptions.
		dir, base := fixture(t)
		commitFile(t, dir, "allowed-ipv6", address+"\n")
		added := strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
		out := string(command(t, dir, binary, base, added))
		for _, commit := range []string{added, "net-diff"} {
			if !strings.Contains(out, `file="fixture.txt" commit=`+commit+" class=ipv6 allowed=true\n") {
				t.Fatalf("missing whole IPv6 exception: %s", out)
			}
		}
		if strings.Contains(out, "class=ipv4") || strings.Contains(out, address) || !strings.Contains(out, "findings=0") {
			t.Fatalf("inaccurate or disclosing whole IPv6 exception report: %s", out)
		}
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
