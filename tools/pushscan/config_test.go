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

func TestCLIContainedLiterals(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "pushscan")
	command(t, ".", "go", "build", "-o", binary, ".")
	for _, mode := range []string{"direct", "external-symlink"} {
		t.Run(mode, func(t *testing.T) {
			dir, base := fixture(t)
			literal := "private-" + "configuration-value"
			contained := filepath.Join(dir, "literals")
			if err := os.WriteFile(contained, []byte(literal), 0600); err != nil {
				t.Fatal("cannot create fixture configuration")
			}
			input := contained
			if mode == "external-symlink" {
				input = filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(contained, input); err != nil {
					t.Fatal("cannot create fixture symlink")
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, base, "HEAD")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PUSHSCAN_LITERALS="+input)
			out, err := cmd.CombinedOutput()
			requireExit(t, err, 2)
			if string(out) != "pushscan: invalid or unreadable configuration\n" || strings.Contains(string(out), literal) {
				t.Fatal("configuration rejection was not generic and redacted")
			}
		})
	}
}
