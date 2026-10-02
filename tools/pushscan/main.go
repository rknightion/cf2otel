// pushscan checks added text in newly reachable commits and the net push diff.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	hexID         = regexp.MustCompile(`\b[0-9a-fA-F]{32}\b`)
	email         = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	ipv4          = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	ipv6          = regexp.MustCompile(`[0-9a-fA-F:.]+`)
	token         = regexp.MustCompile(`\b(?:cfut_|cfapi_|v1\.0-)[A-Za-z0-9_\-]+`)
	documentation = []netip.Prefix{
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
)

type scanner struct {
	ctx      context.Context
	out      io.Writer
	allow    map[string]string
	literals []string
	hits     int
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("pushscan", flag.ContinueOnError)
	// Never echo argv: a mistaken ref or configuration path may contain a secret.
	flags.SetOutput(io.Discard)
	allowFile := flags.String("allowlist", "", "JSON object of exact repository paths to exception reasons")
	if err := flags.Parse(args); err != nil || flags.NArg() != 2 {
		fmt.Fprintln(errOut, "usage: pushscan [--allowlist file] base head")
		return 2
	}
	s := scanner{ctx: ctx, out: out, allow: make(map[string]string)}
	if err := s.configure(*allowFile, os.Getenv("PUSHSCAN_LITERALS")); err != nil {
		fmt.Fprintln(errOut, "pushscan: invalid or unreadable configuration")
		return 2
	}
	base, err := s.resolve(flags.Arg(0))
	if err != nil {
		fmt.Fprintln(errOut, "pushscan: cannot resolve base commit")
		return 2
	}
	head, err := s.resolve(flags.Arg(1))
	if err != nil {
		fmt.Fprintln(errOut, "pushscan: cannot resolve head commit")
		return 2
	}
	commits, err := s.git("rev-list", "--reverse", head, "^"+base)
	if err != nil {
		fmt.Fprintln(errOut, "pushscan: cannot enumerate history")
		return 2
	}
	for _, commit := range strings.Fields(string(commits)) {
		parents, parentErr := s.git("rev-list", "--parents", "-n", "1", commit)
		if parentErr != nil {
			err = parentErr
			break
		}
		refs := strings.Fields(string(parents))
		if len(refs) == 1 {
			// --root compares an initial commit to the empty tree.
			err = s.scanDiff(commit, []string{"diff-tree", "--root", "--no-commit-id", "-r"}, []string{commit})
		} else {
			// Each parent covers merge-resolution additions; side commits are also enumerated above.
			for _, parent := range refs[1:] {
				err = s.scanDiff(commit, []string{"diff"}, []string{parent, commit})
				if err != nil {
					break
				}
			}
		}
		if err != nil {
			break
		}
	}
	if err == nil {
		err = s.scanDiff("net-diff", []string{"diff"}, []string{base, head})
	}
	if err != nil {
		fmt.Fprintln(errOut, "pushscan: git scan failed (no content printed)")
		return 2
	}
	fmt.Fprintf(out, "pushscan: commits=%d findings=%d\n", len(strings.Fields(string(commits))), s.hits)
	if s.hits > 0 {
		return 1
	}
	return 0
}

func (s *scanner) configure(allowFile, literalFile string) error {
	if allowFile != "" {
		data, err := os.ReadFile(allowFile)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &s.allow); err != nil {
			return err
		}
		if s.allow == nil {
			return errors.New("allowlist must be an object")
		}
		for name, reason := range s.allow {
			if name == "" || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "*?[]\\\n\r") || strings.TrimSpace(reason) == "" {
				return errors.New("invalid exact-path exception")
			}
		}
	}
	if literalFile != "" {
		root, err := s.git("rev-parse", "--show-toplevel")
		if err != nil {
			return err
		}
		repository, err := filepath.EvalSymlinks(strings.TrimSpace(string(root)))
		if err != nil {
			return err
		}
		absolute, err := filepath.Abs(literalFile)
		if err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(repository, resolved)
		if err != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
			return errors.New("literal configuration must be external")
		}
		data, err := os.ReadFile(resolved) // #nosec G703 -- canonical external literal-list input
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			value := strings.TrimSuffix(line, "\r")
			if value != "" {
				s.literals = append(s.literals, value)
			}
		}
	}
	return nil
}

func (s *scanner) git(args ...string) ([]byte, error) {
	cmd := exec.CommandContext(s.ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_NO_REPLACE_OBJECTS=1", "GIT_PAGER=cat")
	// Git errors can quote source lines, ref arguments and private paths. Discard stderr.
	return cmd.Output()
}

func (s *scanner) resolve(ref string) (string, error) {
	data, err := s.git("rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return strings.TrimSpace(string(data)), err
}

func (s *scanner) scanDiff(commit string, command, refs []string) error {
	args := append(append([]string{}, command...), "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z")
	args = append(args, refs...)
	args = append(args, "--")
	names, err := s.git(args...)
	if err != nil {
		return err
	}
	for _, name := range bytes.Split(names, []byte{0}) {
		if len(name) == 0 {
			continue
		}
		file := string(name)
		if _, allowed := s.allow[file]; allowed {
			continue
		}
		args = append(append([]string{}, command...), "--no-ext-diff", "--no-textconv", "--no-renames", "--text", "--unified=0", "--no-color")
		args = append(args, refs...)
		// Literal pathspecs prevent special filenames from widening the scan.
		args = append(args, "--", ":(literal)"+file)
		patch, err := s.git(args...)
		if err != nil {
			return err
		}
		inHunk := false
		for _, line := range bytes.Split(patch, []byte{'\n'}) {
			if bytes.HasPrefix(line, []byte("@@ ")) {
				inHunk = true
				continue
			}
			if inHunk && len(line) > 0 && line[0] == '+' {
				for _, class := range s.classes(string(line[1:])) {
					fmt.Fprintf(s.out, "file=%q commit=%s class=%s\n", file, commit, class)
					s.hits++
				}
			}
		}
	}
	return nil
}

func isAllowedAddress(addr netip.Addr) bool {
	// Mapped addresses follow the same loopback and documentation rules as IPv4.
	addr = addr.Unmap()
	if addr.IsLoopback() {
		return true
	}
	for _, prefix := range documentation {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (s *scanner) classes(line string) []string {
	var classes []string
	for _, pattern := range []struct {
		name string
		re   *regexp.Regexp
	}{{"hex-id", hexID}, {"email", email}, {"cloudflare-token", token}} {
		if pattern.re.MatchString(line) {
			classes = append(classes, pattern.name)
		}
	}
	for _, candidate := range ipv4.FindAllString(line, -1) {
		if addr, err := netip.ParseAddr(candidate); err == nil && !isAllowedAddress(addr) {
			classes = append(classes, "ipv4")
			break
		}
	}
	if containsPublicIPv6(line) {
		classes = append(classes, "ipv6")
	}
	for _, literal := range s.literals {
		if strings.Contains(line, literal) {
			classes = append(classes, "literal")
			break
		}
	}
	return classes
}

// Parse bounded slices rather than trusting punctuation-delimited regex matches.
// Longer valid addresses cover their inner slices so allowed addresses do
// not become false positives when their leading groups are removed.
func containsPublicIPv6(line string) bool {
	for _, run := range ipv6.FindAllString(line, -1) {
		coveredEnd := 0
		for start := 0; start < len(run); start++ {
			endLimit := min(len(run), start+45) // maximum textual IPv6 length, including dotted IPv4
			for end := endLimit; end > start; end-- {
				addr, err := netip.ParseAddr(run[start:end])
				if err != nil || !addr.Is6() {
					continue
				}
				if end > coveredEnd {
					if !isAllowedAddress(addr) {
						return true
					}
					coveredEnd = end
				}
				break
			}
		}
	}
	return false
}
