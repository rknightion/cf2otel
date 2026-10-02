package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func command(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "PUSHSCAN_LITERALS=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture command %s: %v", args[0], err)
	}
	return out
}

func requireExit(t *testing.T, err error, code int) {
	t.Helper()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != code {
		t.Fatalf("expected scanner exit %d, got %v", code, err)
	}
}

func fixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	command(t, dir, "git", "init", "-q")
	command(t, dir, "git", "config", "user.name", "fixture")
	command(t, dir, "git", "config", "user.email", "opaque-identity")
	commitFile(t, dir, "initial", "safe\n")
	return dir, strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
}

func commitFile(t *testing.T, dir, message, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "fixture.txt"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	command(t, dir, "git", "add", "--", "fixture.txt")
	command(t, dir, "git", "commit", "-qm", message)
}

func TestCLI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "pushscan")
	command(t, ".", "go", "build", "-o", binary, ".")
	cases := []struct{ name, text, class string }{
		{"hex", strings.Repeat("a1", 16), "hex-id"},
		{"email", "identity" + "@" + "example" + ".com", "email"},
		{"ipv4", strings.Join([]string{"8", "8", "4", "4"}, "."), "ipv4"},
		{"ipv4-before-loopback", strings.Join([]string{"126", "255", "255", "255"}, "."), "ipv4"},
		{"ipv4-after-loopback", strings.Join([]string{"128", "0", "0", "0"}, "."), "ipv4"},
		{"ipv6-outside-loopback", strings.Repeat(":", 2) + "2", "ipv6"},
		{"ipv6", "2606" + ":" + "4700" + strings.Repeat(":", 2) + "1111", "ipv6"},
		{"ipv6-colon-prefix", "endpoint:" + "2606" + ":" + "4700" + strings.Repeat(":", 2) + "1111", "ipv6"},
		{"ipv6-hex-suffix", "2606" + ":" + "4700" + strings.Repeat(":", 2) + "1111" + ".dead", "ipv6"},
		{"token", "cf" + "ut_" + strings.Repeat("x", 20), "cloudflare-token"},
		{"literal", "private-" + "fixture-value", "literal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, base := fixture(t)
			commitFile(t, dir, "add", tc.text+"\n")
			added := strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
			commitFile(t, dir, "remove", "safe\n")
			head := strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
			if diff := command(t, dir, "git", "diff", base, head); len(diff) != 0 {
				t.Fatal("expected net-clean fixture")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, base, head)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PUSHSCAN_LITERALS=")
			if tc.class == "literal" {
				literals := filepath.Join(t.TempDir(), "literals")
				if err := os.WriteFile(literals, []byte(tc.text+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				cmd.Env = append(cmd.Env, "PUSHSCAN_LITERALS="+literals)
			}
			out, err := cmd.CombinedOutput()
			requireExit(t, err, 1)
			if !strings.Contains(string(out), "class="+tc.class) || !strings.Contains(string(out), "commit="+added) {
				t.Fatalf("missing class/commit evidence: %s", out)
			}
			if strings.Contains(string(out), tc.text) {
				t.Fatal("scanner printed matched value")
			}
		})
	}
	t.Run("loopback", func(t *testing.T) {
		for _, tc := range []struct{ name, text string }{
			{"ipv4", strings.Join([]string{"127", "0", "0", "1"}, ".")},
			{"ipv4-range-start", strings.Join([]string{"127", "0", "0", "0"}, ".")},
			{"ipv4-range-end", strings.Join([]string{"127", "255", "255", "255"}, ".")},
			{"ipv6", strings.Repeat(":", 2) + "1"},
			{"ipv6-expanded", strings.Repeat("0:", 7) + "1"},
			{"ipv4-mapped", strings.Repeat(":", 2) + "ffff:" + strings.Join([]string{"127", "0", "0", "1"}, ".")},
		} {
			t.Run(tc.name, func(t *testing.T) {
				dir, base := fixture(t)
				commitFile(t, dir, "loopback", tc.text+"\n")
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				cmd := exec.CommandContext(ctx, binary, base, "HEAD")
				cmd.Dir = dir
				cmd.Env = append(os.Environ(), "PUSHSCAN_LITERALS=")
				out, err := cmd.CombinedOutput()
				cancel()
				if err != nil || !strings.Contains(string(out), "findings=0") {
					t.Fatalf("loopback must pass with no findings: err=%v output=%s", err, out)
				}
			})
		}
	})
	t.Run("documentation-and-old-history", func(t *testing.T) {
		dir, _ := fixture(t)
		commitFile(t, dir, "old", strings.Repeat("a1", 16)+"\n")
		base := strings.TrimSpace(string(command(t, dir, "git", "rev-parse", "HEAD")))
		commitFile(t, dir, "docs", strings.Join([]string{
			strings.Join([]string{"192", "0", "2", "1"}, "."),
			strings.Join([]string{"198", "51", "100", "2"}, "."),
			strings.Join([]string{"203", "0", "113", "3"}, "."),
			"2001" + ":" + "db8" + strings.Repeat(":", 2) + "1",
			"opaque-identity\n",
		}, " "))
		command(t, dir, binary, base, "HEAD")
	})
	t.Run("net-and-explicit-exception", func(t *testing.T) {
		dir, base := fixture(t)
		commitFile(t, dir, "add", strings.Repeat("b2", 16)+"\n")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, base, "HEAD")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PUSHSCAN_LITERALS=")
		out, err := cmd.CombinedOutput()
		requireExit(t, err, 1)
		if !strings.Contains(string(out), "commit=net-diff") {
			t.Fatalf("net diff not rejected: %s", out)
		}
		allow := filepath.Join(t.TempDir(), "allow.json")
		if err := os.WriteFile(allow, []byte(`{"fixture.txt":"synthetic fixture"}`), 0600); err != nil {
			t.Fatal(err)
		}
		command(t, dir, binary, "--allowlist", allow, base, "HEAD")
		if err := os.WriteFile(allow, []byte(`{"fixture.txt":""}`), 0600); err != nil {
			t.Fatal(err)
		}
		cmd = exec.CommandContext(ctx, binary, "--allowlist", allow, base, "HEAD")
		cmd.Dir = dir
		requireExit(t, cmd.Run(), 2)
	})
}
