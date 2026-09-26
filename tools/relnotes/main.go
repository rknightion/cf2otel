package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	conventionalHeader = regexp.MustCompile(`^([a-z][a-z0-9-]*)(?:\(([^()\r\n]+)\))?(!)?:[ \t]+([^\r\n]+)$`)
	markdownListEntry  = regexp.MustCompile(`^(?:[-*+]\s+|[0-9]+[.)]\s+)`)
	stableTag          = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:\+[0-9A-Za-z.-]+)?$`)
)

type requiredCommit struct {
	hash        string
	subject     string
	description string
	present     bool
}

type excludedCommit struct {
	hash       string
	subject    string
	releasedBy string
}

type commitWarning struct {
	hash    string
	subject string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	workingDirectory, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "relnotes: get working directory: %v\n", err)
		return 2
	}
	return runAt(args, workingDirectory, stdout, stderr)
}

func runAt(args []string, repoDirectory string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("relnotes", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rangeSpec := flags.String("range", "", "tag..ref range to check; defaults to the newest stable v* tag reachable from HEAD")
	notesPath := flags.String("notes", "", "path to the release notes text file")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "relnotes: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 2
	}
	if strings.TrimSpace(*notesPath) == "" {
		fmt.Fprintln(stderr, "relnotes: --notes is required")
		return 2
	}
	notes, err := os.ReadFile(*notesPath)
	if err != nil {
		fmt.Fprintf(stderr, "relnotes: read notes file: %v\n", err)
		return 2
	}

	releaseTag, releaseSHA, _, targetSHA, label, err := resolveRange(repoDirectory, *rangeSpec)
	if err != nil {
		fmt.Fprintf(stderr, "relnotes: %v\n", err)
		return 2
	}

	newCommits, err := gitLines(repoDirectory, "rev-list", "--reverse", "--topo-order", releaseSHA+".."+targetSHA)
	if err != nil {
		fmt.Fprintf(stderr, "relnotes: list commits in %s: %v\n", label, err)
		return 2
	}
	candidates := make([]requiredCommit, 0)
	excluded := make([]excludedCommit, 0)
	warnings := make([]commitWarning, 0)
	for _, hash := range newCommits {
		subject, subjectErr := gitSubject(repoDirectory, hash)
		if subjectErr != nil {
			fmt.Fprintf(stderr, "relnotes: read subject for commit %s: %v\n", hash, subjectErr)
			return 2
		}
		commitType, description, conventional := parseConventional(subject)
		if !conventional {
			warnings = append(warnings, commitWarning{hash: hash, subject: subject})
			continue
		}
		if commitType != "feat" && commitType != "fix" && commitType != "perf" {
			continue
		}
		candidates = append(candidates, requiredCommit{
			hash:        hash,
			subject:     subject,
			description: description,
		})
	}

	required := make([]requiredCommit, 0, len(candidates))
	if len(candidates) > 0 {
		candidateHashes := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			candidateHashes = append(candidateHashes, candidate.hash)
		}
		candidatePatchIDs, patchErr := gitPatchIDs(repoDirectory, candidateHashes)
		if patchErr != nil {
			fmt.Fprintf(stderr, "relnotes: patch-id for candidate commits: %v\n", patchErr)
			return 2
		}
		releasedPatchIDs := make(map[string]string)
		needsReleasedComparison := false
		for _, patchID := range candidatePatchIDs {
			if patchID != "" {
				needsReleasedComparison = true
				break
			}
		}
		if needsReleasedComparison {
			releasedCommits, listErr := gitLines(repoDirectory, "rev-list", releaseSHA)
			if listErr != nil {
				fmt.Fprintf(stderr, "relnotes: list commits reachable from %s: %v\n", releaseTag, listErr)
				return 2
			}
			releasedPatchIDsByHash, releasedErr := gitPatchIDs(repoDirectory, releasedCommits)
			if releasedErr != nil {
				fmt.Fprintf(stderr, "relnotes: patch-id for released commits: %v\n", releasedErr)
				return 2
			}
			for _, hash := range releasedCommits {
				patchID := releasedPatchIDsByHash[hash]
				if patchID != "" {
					if _, ok := releasedPatchIDs[patchID]; !ok {
						releasedPatchIDs[patchID] = hash
					}
				}
			}
		}

		for _, candidate := range candidates {
			patchID := candidatePatchIDs[candidate.hash]
			if releasedBy, ok := releasedPatchIDs[patchID]; patchID != "" && ok {
				excluded = append(excluded, excludedCommit{hash: candidate.hash, subject: candidate.subject, releasedBy: releasedBy})
				continue
			}
			required = append(required, candidate)
		}
	}

	availableMentions := make(map[string]int)
	for _, candidate := range required {
		normalizedDescription := normalizeText(candidate.description)
		if _, ok := availableMentions[normalizedDescription]; !ok {
			availableMentions[normalizedDescription] = countNoteEntries(string(notes), normalizedDescription)
		}
	}
	for index := range required {
		normalizedDescription := normalizeText(required[index].description)
		if availableMentions[normalizedDescription] > 0 {
			required[index].present = true
			availableMentions[normalizedDescription]--
		}
	}

	fmt.Fprintf(stdout, "Release notes coverage for %s:\n", label)
	if len(required) == 0 {
		fmt.Fprintln(stdout, "Required commits: none")
	} else {
		fmt.Fprintln(stdout, "Required commits:")
	}
	missing := 0
	for _, commit := range required {
		status := "PRESENT"
		if !commit.present {
			status = "MISSING"
			missing++
		}
		fmt.Fprintf(stdout, "  %s %s %s\n", status, commit.hash, commit.subject)
		if !commit.present {
			fmt.Fprintf(stdout, "    restore: git commit --allow-empty -m %s\n", shellQuote(commit.subject))
		}
	}
	if len(excluded) > 0 {
		fmt.Fprintln(stdout, "Excluded patch-identical commits:")
		for _, commit := range excluded {
			fmt.Fprintf(stdout, "  EXCLUDED patch-identical %s %s (matches released commit %s)\n", commit.hash, commit.subject, commit.releasedBy)
		}
	}
	if len(warnings) > 0 {
		fmt.Fprintln(stdout, "Warnings (non-conventional subjects; informational):")
		for _, warning := range warnings {
			fmt.Fprintf(stdout, "  WARNING %s %s\n", warning.hash, warning.subject)
		}
	}
	if missing > 0 {
		fmt.Fprintf(stdout, "%d required commit subject(s) missing from the release notes\n", missing)
		return 1
	}
	fmt.Fprintf(stdout, "All %d required commit subject(s) are present\n", len(required))
	return 0
}

func resolveRange(repoDirectory, rangeSpec string) (tag, tagSHA, targetRef, targetSHA, label string, err error) {
	if rangeSpec == "" {
		targetRef = "HEAD"
		targetSHA, err = resolveCommit(repoDirectory, targetRef)
		if err != nil {
			return "", "", "", "", "", fmt.Errorf("resolve default target HEAD: %w", err)
		}
		tag, tagSHA, err = newestStableTag(repoDirectory, targetSHA)
		if err != nil {
			return "", "", "", "", "", err
		}
		return tag, tagSHA, targetRef, targetSHA, tag + ".." + targetRef, nil
	}
	if strings.Count(rangeSpec, "..") != 1 {
		return "", "", "", "", "", fmt.Errorf("invalid --range %q: expected <last-release-tag>..<ref>", rangeSpec)
	}
	parts := strings.SplitN(rangeSpec, "..", 2)
	tag, targetRef = parts[0], parts[1]
	if strings.TrimSpace(tag) != tag || strings.TrimSpace(targetRef) != targetRef || tag == "" || targetRef == "" {
		return "", "", "", "", "", fmt.Errorf("invalid --range %q: both references must be non-empty and trimmed", rangeSpec)
	}
	tagRef := tag
	if !strings.HasPrefix(tagRef, "refs/tags/") {
		tagRef = "refs/tags/" + tagRef
	}
	tagSHA, err = resolveCommit(repoDirectory, tagRef)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("resolve release tag %q: %w", tag, err)
	}
	targetSHA, err = resolveCommit(repoDirectory, targetRef)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("resolve target %q: %w", targetRef, err)
	}
	return tag, tagSHA, targetRef, targetSHA, rangeSpec, nil
}

func newestStableTag(repoDirectory, targetSHA string) (string, string, error) {
	tags, err := gitLines(repoDirectory, "tag", "--list", "v*", "--sort=-version:refname")
	if err != nil {
		return "", "", fmt.Errorf("list version tags: %w", err)
	}
	for _, tag := range tags {
		if !stableTag.MatchString(tag) {
			continue
		}
		tagSHA, resolveErr := resolveCommit(repoDirectory, "refs/tags/"+tag)
		if resolveErr != nil {
			return "", "", fmt.Errorf("resolve stable tag %q: %w", tag, resolveErr)
		}
		ancestor, ancestorErr := isAncestor(repoDirectory, tagSHA, targetSHA)
		if ancestorErr != nil {
			return "", "", fmt.Errorf("check whether stable tag %q reaches HEAD: %w", tag, ancestorErr)
		}
		if ancestor {
			return tag, tagSHA, nil
		}
	}
	return "", "", errors.New("no reachable stable v* release tag found")
}

func isAncestor(repoDirectory, ancestor, descendant string) (bool, error) {
	cmd := exec.Command("git", "-C", repoDirectory, "merge-base", "--is-ancestor", ancestor, descendant)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor: %w: %s", err, strings.TrimSpace(string(output)))
}

func resolveCommit(repoDirectory, revision string) (string, error) {
	resolved, err := gitOutput(repoDirectory, "rev-parse", "--verify", "--quiet", "--end-of-options", revision+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resolved), nil
}

func gitLines(repoDirectory string, args ...string) ([]string, error) {
	output, err := gitOutput(repoDirectory, args...)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	return lines, nil
}

func gitOutput(repoDirectory string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", repoDirectory}, args...)
	cmd := exec.Command("git", commandArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimRight(string(output), "\r\n"), nil
}

func gitSubject(repoDirectory, hash string) (string, error) {
	return gitOutput(repoDirectory, "show", "-s", "--format=%s", "--no-show-signature", hash)
}

func gitPatchIDs(repoDirectory string, hashes []string) (map[string]string, error) {
	patchIDs := make(map[string]string, len(hashes))
	for start := 0; start < len(hashes); start += 128 {
		end := start + 128
		if end > len(hashes) {
			end = len(hashes)
		}
		commandArgs := []string{"-C", repoDirectory, "show", "--first-parent", "--no-walk", "--format=commit %H", "--binary", "--no-show-signature"}
		commandArgs = append(commandArgs, hashes[start:end]...)
		show := exec.Command("git", commandArgs...)
		patch := exec.Command("git", "-C", repoDirectory, "patch-id", "--stable")
		pipe, err := show.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("git show stdout pipe: %w", err)
		}
		patch.Stdin = pipe
		var showStderr, patchOutput, patchStderr bytes.Buffer
		show.Stderr = &showStderr
		patch.Stdout = &patchOutput
		patch.Stderr = &patchStderr
		if err := show.Start(); err != nil {
			return nil, fmt.Errorf("git show: %w", err)
		}
		if err := patch.Start(); err != nil {
			_ = show.Process.Kill()
			_ = show.Wait()
			return nil, fmt.Errorf("git patch-id --stable: %w", err)
		}
		patchErr := patch.Wait()
		showErr := show.Wait()
		if patchErr != nil {
			return nil, fmt.Errorf("git patch-id --stable: %w: %s", patchErr, strings.TrimSpace(patchStderr.String()))
		}
		if showErr != nil {
			return nil, fmt.Errorf("git show: %w: %s", showErr, strings.TrimSpace(showStderr.String()))
		}
		if patchStderr.Len() > 0 {
			return nil, fmt.Errorf("git patch-id --stable: %s", strings.TrimSpace(patchStderr.String()))
		}
		for _, line := range strings.Split(strings.TrimSpace(patchOutput.String()), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			patchIDs[fields[1]] = fields[0]
		}
	}
	return patchIDs, nil
}

func parseConventional(subject string) (string, string, bool) {
	match := conventionalHeader.FindStringSubmatch(subject)
	if match == nil {
		return "", "", false
	}
	description := strings.TrimSpace(match[4])
	if description == "" {
		return "", "", false
	}
	return match[1], description, true
}

func normalizeText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func containsWholePhrase(text, phrase string) bool {
	if phrase == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(phrase)
	last, _ := utf8.DecodeLastRuneInString(phrase)
	for offset := 0; offset <= len(text); {
		relative := strings.Index(text[offset:], phrase)
		if relative < 0 {
			return false
		}
		start := offset + relative
		end := start + len(phrase)
		before := runeBefore(text, start)
		after := runeAfter(text, end)
		startBoundary := !isWordRune(first) || !isWordRune(before)
		endBoundary := !isWordRune(last) || !isWordRune(after)
		if startBoundary && endBoundary {
			return true
		}
		offset = end
	}
	return false
}

func countNoteEntries(notes, normalizedDescription string) int {
	if normalizedDescription == "" {
		return 0
	}
	entries := make([]string, 0)
	current := ""
	flush := func() {
		if strings.TrimSpace(current) != "" {
			entries = append(entries, current)
		}
		current = ""
	}
	for _, line := range strings.Split(notes, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flush()
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			flush()
			continue
		}
		if markdownListEntry.MatchString(trimmed) {
			flush()
			current = markdownListEntry.ReplaceAllString(trimmed, "")
			continue
		}
		if current == "" {
			current = trimmed
		} else {
			current += " " + trimmed
		}
	}
	flush()

	count := 0
	for _, entry := range entries {
		if containsWholePhrase(normalizeText(entry), normalizedDescription) {
			count++
		}
	}
	return count
}

func runeBefore(value string, offset int) rune {
	if offset == 0 {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(value[:offset])
	return r
}

func runeAfter(value string, offset int) rune {
	if offset == len(value) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(value[offset:])
	return r
}

func isWordRune(value rune) bool {
	return value == '_' || unicode.IsLetter(value) || unicode.IsNumber(value)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
