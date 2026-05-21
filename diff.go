package main

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var hunkHeaderRE = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

type changeKind string

// changeKind mirrors git diff status letters. T is a type change.
const (
	changeAdded    changeKind = "A"
	changeDeleted  changeKind = "D"
	changeModified changeKind = "M"
	changeRenamed  changeKind = "R"
	changeType     changeKind = "T"
)

type testFileChange struct {
	Kind    changeKind
	OldPath string
	NewPath string
}

func (change testFileChange) displayPath() string {
	return cmp.Or(change.NewPath, change.OldPath)
}

func (change testFileChange) pathspecs() []string {
	oldPath := cmp.Or(change.OldPath, change.NewPath)
	newPath := cmp.Or(change.NewPath, change.OldPath)
	if oldPath == "" {
		return []string{newPath}
	}
	if newPath == "" || newPath == oldPath {
		return []string{oldPath}
	}
	return []string{oldPath, newPath}
}

// lineRange uses End < Start to represent an empty span from a zero-count diff
// hunk. hasLines reports whether the span contains any real source lines.
type lineRange struct {
	Start int
	End   int
}

type diffHunk struct {
	Old lineRange
	New lineRange
}

func newSideOnlyHunks(hunks []diffHunk) []diffHunk {
	trimmed := make([]diffHunk, 0, len(hunks))
	for _, hunk := range hunks {
		hunk.Old = lineRange{}
		trimmed = append(trimmed, hunk)
	}
	return trimmed
}

func listChangedTestFiles(ctx context.Context, cfg config, git gitRunner) ([]testFileChange, error) {
	result, err := git(
		ctx,
		cfg.RepoRoot,
		"diff",
		"--name-status",
		"-z",
		"--find-renames",
		"--diff-filter=ADMRT",
		diffRangeSpec(cfg),
	)
	if err != nil {
		return nil, err
	}
	if result.Stdout == "" {
		return nil, nil
	}

	fields := strings.Split(result.Stdout, "\x00")
	changes := make([]testFileChange, 0)
	for index := 0; index < len(fields); {
		status := fields[index]
		index++
		if status == "" {
			continue
		}
		kind, err := parseChangeKind(status)
		if err != nil {
			return nil, err
		}
		switch kind {
		case changeRenamed:
			if index+1 >= len(fields) {
				return nil, fmt.Errorf("rename status %q is missing paths", status)
			}
			oldPath := cleanGitPath(fields[index])
			newPath := cleanGitPath(fields[index+1])
			index += 2
			change := testFileChange{Kind: kind, OldPath: oldPath, NewPath: newPath}
			if !isRunnableTestFilePath(change.OldPath) && !isRunnableTestFilePath(change.NewPath) {
				continue
			}
			changes = append(changes, change)
		default:
			if index >= len(fields) {
				return nil, fmt.Errorf("status %q is missing a path", status)
			}
			path := cleanGitPath(fields[index])
			index++
			change := testFileChange{Kind: kind, OldPath: path, NewPath: path}
			switch kind {
			case changeAdded:
				change.OldPath = ""
			case changeDeleted:
				change.NewPath = ""
			}
			if !isRunnableTestFilePath(change.displayPath()) {
				continue
			}
			changes = append(changes, change)
		}
	}
	slices.SortFunc(changes, func(left, right testFileChange) int {
		return cmp.Compare(left.displayPath(), right.displayPath())
	})
	return changes, nil
}

func parseChangeKind(status string) (changeKind, error) {
	switch {
	case strings.HasPrefix(status, string(changeAdded)):
		return changeAdded, nil
	case strings.HasPrefix(status, string(changeDeleted)):
		return changeDeleted, nil
	case strings.HasPrefix(status, string(changeModified)):
		return changeModified, nil
	case strings.HasPrefix(status, string(changeRenamed)):
		return changeRenamed, nil
	case strings.HasPrefix(status, string(changeType)):
		return changeType, nil
	default:
		return "", fmt.Errorf("unsupported diff status %q", status)
	}
}

func cleanGitPath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func isRunnableTestFilePath(path string) bool {
	if !strings.HasSuffix(path, "_test.go") {
		return false
	}
	cleanPath := cleanGitPath(path)
	baseName := filepath.Base(cleanPath)
	if strings.HasPrefix(baseName, ".") || strings.HasPrefix(baseName, "_") {
		return false
	}
	for segment := range strings.SplitSeq(filepath.ToSlash(filepath.Dir(cleanPath)), "/") {
		if segment == "." || segment == "" {
			continue
		}
		if segment == "testdata" || segment == "vendor" || strings.HasPrefix(segment, ".") || strings.HasPrefix(segment, "_") {
			return false
		}
	}
	return true
}

func listDiffHunks(ctx context.Context, cfg config, git gitRunner, change testFileChange) ([]diffHunk, error) {
	args := []string{"diff", "--unified=0", "--no-color", "--find-renames", diffRangeSpec(cfg), "--"}
	args = append(args, change.pathspecs()...)
	result, err := git(ctx, cfg.RepoRoot, args...)
	if err != nil {
		return nil, err
	}
	return parseDiffHunks(result.Stdout)
}

func parseDiffHunks(diff string) ([]diffHunk, error) {
	hunks := make([]diffHunk, 0)
	for line := range strings.Lines(diff) {
		line = strings.TrimSuffix(line, "\n")
		matches := hunkHeaderRE.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		oldRange, err := parseRange(matches[1], matches[2])
		if err != nil {
			return nil, err
		}
		newRange, err := parseRange(matches[3], matches[4])
		if err != nil {
			return nil, err
		}
		hunks = append(hunks, diffHunk{Old: oldRange, New: newRange})
	}
	return hunks, nil
}

func parseRange(startText, countText string) (lineRange, error) {
	start, err := parseNonNegativeInt(startText)
	if err != nil {
		return lineRange{}, err
	}
	count := 1
	if countText != "" {
		count, err = parseNonNegativeInt(countText)
		if err != nil {
			return lineRange{}, err
		}
	}
	if count == 0 {
		if start == 0 {
			start = 1
		}
		return lineRange{Start: start, End: start - 1}, nil
	}
	return lineRange{Start: start, End: start + count - 1}, nil
}

func parseNonNegativeInt(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse integer %q: %w", value, err)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("negative value %q", value)
	}
	return parsed, nil
}

func readFileAtRevision(ctx context.Context, cfg config, git gitRunner, revision, filePath string) ([]byte, bool, error) {
	if err := ensureRevisionExists(ctx, cfg, git, revision); err != nil {
		return nil, false, err
	}
	fileExists, err := fileExistsAtRevision(ctx, cfg, git, revision, filePath)
	if err != nil {
		return nil, false, err
	}
	if !fileExists {
		return nil, false, nil
	}

	result, err := git(ctx, cfg.RepoRoot, "show", revision+":"+filePath)
	if err != nil {
		return nil, false, fmt.Errorf("read %s at %s: %w", filePath, revision, err)
	}
	return []byte(result.Stdout), true, nil
}

func fileExistsAtRevision(ctx context.Context, cfg config, git gitRunner, revision, filePath string) (bool, error) {
	result, err := git(ctx, cfg.RepoRoot, "ls-tree", "-z", "--name-only", revision, "--", filePath)
	if err != nil {
		return false, fmt.Errorf("check whether %s exists at %s: %w", filePath, revision, err)
	}
	cleanPath := cleanGitPath(filePath)
	for part := range strings.SplitSeq(result.Stdout, "\x00") {
		if part == "" {
			continue
		}
		if cleanGitPath(part) == cleanPath {
			return true, nil
		}
	}
	return false, nil
}

func (r lineRange) hasLines() bool {
	return r.Start > 0 && r.End >= r.Start
}

func (r lineRange) overlaps(other lineRange) bool {
	if !r.hasLines() || !other.hasLines() {
		return false
	}
	return r.Start <= other.End && other.Start <= r.End
}
