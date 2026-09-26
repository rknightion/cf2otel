package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPreTagDatedFeatureMergedAfterTagIsRequired(t *testing.T) {
	repo := newTestRepo(t)
	writeRepoFile(t, repo, "base.txt", "base\n")
	commitAt(t, repo, "chore: start history", "2026-01-01T00:00:00Z")
	if err := git(t, repo, "branch", "feature"); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "release.txt", "release\n")
	commitAt(t, repo, "chore: release baseline", "2026-01-03T00:00:00Z")
	if err := git(t, repo, "tag", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	tagDate := gitOutputForTest(t, repo, "show", "-s", "--format=%cI", "v1.0.0")
	if err := git(t, repo, "switch", "feature"); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "feature.txt", "email routing\n")
	commitAt(t, repo, "feat(email): Email Routing and Sending", "2026-01-02T00:00:00Z")
	feature := gitOutputForTest(t, repo, "rev-parse", "HEAD")
	featureDate := gitOutputForTest(t, repo, "show", "-s", "--format=%cI", "HEAD")
	if !parseDate(t, featureDate).Before(parseDate(t, tagDate)) {
		t.Fatalf("fixture feature date %q is not before tag date %q", featureDate, tagDate)
	}
	if err := git(t, repo, "switch", "main"); err != nil {
		t.Fatal(err)
	}
	if err := git(t, repo, "merge", "--no-ff", "feature", "-m", "Merge feature branch"); err != nil {
		t.Fatal(err)
	}
	if err := git(t, repo, "merge-base", "--is-ancestor", feature, "HEAD"); err != nil {
		t.Fatalf("feature commit is not reachable after the merge: %v", err)
	}
	if err := git(t, repo, "merge-base", "--is-ancestor", feature, "v1.0.0"); err == nil {
		t.Fatal("feature commit unexpectedly belongs to the release tag")
	}

	code, output := checkNotes(t, repo, "v1.0.0..HEAD", "# Release notes\n\n### Features\n")
	if code != 1 {
		t.Fatalf("missing feature returned exit %d, want 1\n%s", code, output)
	}
	for _, want := range []string{
		"MISSING",
		"feat(email): Email Routing and Sending",
		"git commit --allow-empty -m 'feat(email): Email Routing and Sending'",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestPatchIdenticalReleasedFixIsExcluded(t *testing.T) {
	repo := newTestRepo(t)
	writeRepoFile(t, repo, "base.txt", "base\n")
	commitAt(t, repo, "chore: start history", "2026-01-01T00:00:00Z")
	base := gitOutputForTest(t, repo, "rev-parse", "HEAD")
	writeRepoFile(t, repo, "cache.txt", "fixed\n")
	commitAt(t, repo, "fix(cache): handle stale values", "2026-01-02T00:00:00Z")
	releasedFix := gitOutputForTest(t, repo, "rev-parse", "HEAD")
	if err := git(t, repo, "tag", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := git(t, repo, "switch", "-c", "duplicate", base); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "cache.txt", "fixed\n")
	commitAt(t, repo, "fix(cache): duplicate released patch", "2026-01-03T00:00:00Z")
	duplicate := gitOutputForTest(t, repo, "rev-parse", "HEAD")
	if err := git(t, repo, "switch", "main"); err != nil {
		t.Fatal(err)
	}
	if err := git(t, repo, "merge", "--no-ff", "duplicate", "-m", "Merge duplicate patch"); err != nil {
		t.Fatal(err)
	}
	if patchID(t, repo, releasedFix) != patchID(t, repo, duplicate) {
		t.Fatal("fixture commits are not patch-identical")
	}

	code, output := checkNotes(t, repo, "v1.0.0..HEAD", "# Release notes\n")
	if code != 0 {
		t.Fatalf("patch-identical released fix made the check fail with exit %d:\n%s", code, output)
	}
	if !strings.Contains(output, "EXCLUDED patch-identical") || !strings.Contains(output, "fix(cache): duplicate released patch") {
		t.Fatalf("output does not identify the excluded patch-equivalent fix:\n%s", output)
	}
	if strings.Contains(output, "MISSING") || strings.Contains(output, "restore: git commit") {
		t.Fatalf("excluded fix was treated as a missing release note:\n%s", output)
	}
}

func TestCompleteNotesPassWithScopeCaseAndTrimDifferences(t *testing.T) {
	repo := newTestRepo(t)
	writeRepoFile(t, repo, "base.txt", "base\n")
	commitAt(t, repo, "chore: start history", "2026-01-01T00:00:00Z")
	if err := git(t, repo, "tag", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "feature.txt", "feature\n")
	commitAt(t, repo, "feat: Add email export", "2026-01-02T00:00:00Z")
	writeRepoFile(t, repo, "fix.txt", "fixed\n")
	commitAt(t, repo, "fix(api)!: handle empty response", "2026-01-03T00:00:00Z")
	writeRepoFile(t, repo, "perf.txt", "fast\n")
	commitAt(t, repo, "perf(cache): avoid duplicate scans", "2026-01-04T00:00:00Z")
	writeRepoFile(t, repo, "docs.txt", "docs\n")
	commitAt(t, repo, "Update docs without a conventional type", "2026-01-05T00:00:00Z")

	notes := "# Release notes\n\n### Features\n*   ADD EMAIL EXPORT   \n\n### Fixes\n* Handle empty response\n\n### Performance\n* Avoid duplicate scans\n"
	code, output := checkNotes(t, repo, "v1.0.0..HEAD", notes)
	if code != 0 {
		t.Fatalf("complete notes returned exit %d:\n%s", code, output)
	}
	for _, want := range []string{"PRESENT", "WARNING", "Update docs without a conventional type"} {
		if !strings.Contains(output, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "MISSING") || strings.Contains(output, "restore: git commit") {
		t.Fatalf("complete notes were marked missing:\n%s", output)
	}
}

func TestDefaultRangeUsesNewestStableTagReachableFromHead(t *testing.T) {
	repo := newTestRepo(t)
	writeRepoFile(t, repo, "base.txt", "base\n")
	commitAt(t, repo, "chore: start history", "2026-01-01T00:00:00Z")
	if err := git(t, repo, "tag", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "one.txt", "one\n")
	commitAt(t, repo, "fix: first stable release change", "2026-01-02T00:00:00Z")
	if err := git(t, repo, "tag", "v1.1.0"); err != nil {
		t.Fatal(err)
	}
	if err := git(t, repo, "tag", "v2.0.0-rc.1"); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "two.txt", "two\n")
	commitAt(t, repo, "feat: add post-release feature", "2026-01-03T00:00:00Z")
	notes := "# Release notes\n* Add post-release feature\n"
	code, output := checkNotes(t, repo, "", notes)
	if code != 0 {
		t.Fatalf("default range returned exit %d:\n%s", code, output)
	}
	if !strings.Contains(output, "v1.1.0..HEAD") || strings.Contains(output, "first stable release change") {
		t.Fatalf("default range did not select the newest stable tag:\n%s", output)
	}
}

func TestShortSubjectInsideAnotherWordIsMissing(t *testing.T) {
	repo := newTestRepo(t)
	writeRepoFile(t, repo, "base.txt", "base\n")
	commitAt(t, repo, "chore: start history", "2026-01-01T00:00:00Z")
	if err := git(t, repo, "tag", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "feature.txt", "authorization\n")
	commitAt(t, repo, "feat: auth", "2026-01-02T00:00:00Z")

	code, output := checkNotes(t, repo, "v1.0.0..HEAD", "# Notes\n* Add author aliases\n")
	if code != 1 || !strings.Contains(output, "MISSING") {
		t.Fatalf("substring inside another word counted as a mention, exit %d:\n%s", code, output)
	}
}

func TestDuplicateNormalizedDescriptionsNeedDistinctEntries(t *testing.T) {
	repo := newTestRepo(t)
	writeRepoFile(t, repo, "base.txt", "base\n")
	commitAt(t, repo, "chore: start history", "2026-01-01T00:00:00Z")
	if err := git(t, repo, "tag", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "feature.txt", "feature\n")
	commitAt(t, repo, "feat(api): Add email export", "2026-01-02T00:00:00Z")
	writeRepoFile(t, repo, "fix.txt", "fixed\n")
	commitAt(t, repo, "fix(worker): ADD EMAIL EXPORT", "2026-01-03T00:00:00Z")

	code, output := checkNotes(t, repo, "v1.0.0..HEAD", "# Release notes\n\n* Add email export\n")
	if code != 1 {
		t.Fatalf("one note entry satisfied two required commits, returned exit %d:\n%s", code, output)
	}
	if strings.Count(output, "PRESENT ") != 1 || strings.Count(output, "MISSING ") != 1 {
		t.Fatalf("one matching entry should cover only one commit:\n%s", output)
	}

	code, output = checkNotes(t, repo, "v1.0.0..HEAD", "# Release notes\n\n* Add email export\n* Add Email Export\n")
	if code != 0 {
		t.Fatalf("two distinct entries did not cover both commits, returned exit %d:\n%s", code, output)
	}
}

func TestEmptyRangeSkipsPatchIDScanning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git wrapper uses a POSIX shell")
	}
	repo := newTestRepo(t)
	writeRepoFile(t, repo, "base.txt", "base\n")
	commitAt(t, repo, "chore: start history", "2026-01-01T00:00:00Z")
	for index := 0; index < 4; index++ {
		writeRepoFile(t, repo, fmt.Sprintf("history-%d.txt", index), fmt.Sprintf("entry %d\n", index))
		commitAt(t, repo, fmt.Sprintf("fix: released change %d", index), fmt.Sprintf("2026-01-%02dT00:00:00Z", index+2))
	}
	if err := git(t, repo, "tag", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	binDirectory := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "git-calls.log")
	wrapper := "#!/bin/sh\nfor arg do\n  case \"$arg\" in show|patch-id) printf '%s\\n' \"$arg\" >> \"$RELNOTES_GIT_LOG\";; esac\ndone\nexec \"$RELNOTES_REAL_GIT\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binDirectory, "git"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RELNOTES_GIT_LOG", logPath)
	t.Setenv("RELNOTES_REAL_GIT", realGit)
	t.Setenv("PATH", binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))

	code, output := checkNotes(t, repo, "v1.0.0..v1.0.0", "# Release notes\n")
	if code != 0 {
		t.Fatalf("empty range returned exit %d:\n%s", code, output)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("empty range scanned released history through git show/patch-id: %s", strings.TrimSpace(string(calls)))
	}
}

func newTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := git(t, repo, "init", "--initial-branch=main"); err != nil {
		t.Fatal(err)
	}
	if err := git(t, repo, "config", "user.name", "Example User"); err != nil {
		t.Fatal(err)
	}
	if err := git(t, repo, "config", "user.email", "test@example.com"); err != nil {
		t.Fatal(err)
	}
	return repo
}

func writeRepoFile(t *testing.T, repo, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func commitAt(t *testing.T, repo, subject, date string) {
	t.Helper()
	if err := gitWithEnv(t, repo, map[string]string{"GIT_AUTHOR_DATE": date, "GIT_COMMITTER_DATE": date}, "add", "--all"); err != nil {
		t.Fatal(err)
	}
	if err := gitWithEnv(t, repo, map[string]string{"GIT_AUTHOR_DATE": date, "GIT_COMMITTER_DATE": date}, "commit", "-m", subject); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, repo string, args ...string) error {
	t.Helper()
	return gitWithEnv(t, repo, nil, args...)
}

func gitWithEnv(t *testing.T, repo string, values map[string]string, args ...string) error {
	t.Helper()
	command := append([]string{"-C", repo}, args...)
	cmd := exec.Command("git", command...)
	if values != nil {
		cmd.Env = append(os.Environ(), envPairs(values)...)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func gitOutputForTest(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := append([]string{"-C", repo}, args...)
	output, err := exec.Command("git", command...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output))
}

func checkNotes(t *testing.T, repo, rangeSpec, notes string) (int, string) {
	t.Helper()
	notesPath := filepath.Join(t.TempDir(), "release-notes.txt")
	if err := os.WriteFile(notesPath, []byte(notes), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--notes", notesPath}
	if rangeSpec != "" {
		args = append(args, "--range", rangeSpec)
	}
	var stdout, stderr bytes.Buffer
	code := runAt(args, repo, &stdout, &stderr)
	return code, stdout.String() + stderr.String()
}

func patchID(t *testing.T, repo, commit string) string {
	t.Helper()
	patch := gitOutputForTest(t, repo, "show", "--first-parent", "--format=", "--binary", commit)
	cmd := exec.Command("git", "-C", repo, "patch-id", "--stable")
	cmd.Stdin = strings.NewReader(patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git patch-id: %v: %s", err, strings.TrimSpace(string(output)))
	}
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func parseDate(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse date %q: %v", value, err)
	}
	return parsed
}

func envPairs(values map[string]string) []string {
	pairs := make([]string, 0, len(values))
	for key, value := range values {
		pairs = append(pairs, key+"="+value)
	}
	return pairs
}
