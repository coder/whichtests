package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunWithRealGitHandlesAddedFileAtRevision(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test User")
	runGit(t, repoRoot, "commit", "--allow-empty", "-m", "base")
	baseSHA := strings.TrimSpace(runGit(t, repoRoot, "rev-parse", "HEAD"))

	writeTestFile(t, repoRoot, "pkg/new_test.go", `package sample

import "testing"

func TestAdded(t *testing.T) {
	t.Log("added")
}
`)
	runGit(t, repoRoot, "add", ".")
	runGit(t, repoRoot, "commit", "-m", "head")
	headSHA := strings.TrimSpace(runGit(t, repoRoot, "rev-parse", "HEAD"))

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	summaryPath := filepath.Join(repoRoot, "summary.md")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(t.Context(), config{RepoRoot: repoRoot, BaseSHA: baseSHA, HeadSHA: headSHA, OutMatrix: matrixPath, OutSummary: summaryPath}, &stdout, &stderr, execGit)
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 1)
	require.Equal(t, "./pkg", matrix.Include[0].Package)
	require.Equal(t, "^(TestAdded)(/.*)?$", matrix.Include[0].RunRegex)

	summary, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.Contains(t, string(summary), "TestAdded")
}

func TestRunWithRealGitHandlesDeletedSetupFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test User")
	writeTestFile(t, repoRoot, "pkg/setup_test.go", `package sample

import "testing"

func setup(t *testing.T) {
	t.Helper()
}
`)
	writeTestFile(t, repoRoot, "pkg/alpha_test.go", `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`)
	runGit(t, repoRoot, "add", ".")
	runGit(t, repoRoot, "commit", "-m", "base")
	baseSHA := strings.TrimSpace(runGit(t, repoRoot, "rev-parse", "HEAD"))

	runGit(t, repoRoot, "rm", "pkg/setup_test.go")
	runGit(t, repoRoot, "commit", "-m", "head")
	headSHA := strings.TrimSpace(runGit(t, repoRoot, "rev-parse", "HEAD"))

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	summaryPath := filepath.Join(repoRoot, "summary.md")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(t.Context(), config{RepoRoot: repoRoot, BaseSHA: baseSHA, HeadSHA: headSHA, OutMatrix: matrixPath, OutSummary: summaryPath}, &stdout, &stderr, execGit)
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 1)
	require.Equal(t, "./pkg", matrix.Include[0].Package)
	require.Equal(t, "^(TestAlpha)(/.*)?$", matrix.Include[0].RunRegex)

	summary, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.Contains(t, string(summary), "pkg/setup_test.go")
	require.Contains(t, string(summary), "TestAlpha")
}

func TestEnsureRangeAvailableWithRealGitFetchesMovedBase(t *testing.T) {
	t.Parallel()

	requireGit(t)
	root := t.TempDir()
	workRoot := filepath.Join(root, "work")
	bareRoot := filepath.Join(root, "upstream.git")
	cloneRoot := filepath.Join(root, "clone")
	require.NoError(t, os.MkdirAll(workRoot, 0o750))
	runGit(t, workRoot, "init")
	runGit(t, workRoot, "config", "user.email", "test@example.com")
	runGit(t, workRoot, "config", "user.name", "Test User")
	writeTestFile(t, workRoot, "pkg/sample_test.go", `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("base")
}
`)
	runGit(t, workRoot, "add", ".")
	runGit(t, workRoot, "commit", "-m", "base")
	runGit(t, workRoot, "branch", "-M", "main")

	runGit(t, workRoot, "checkout", "-b", "feature")
	writeTestFile(t, workRoot, "pkg/sample_test.go", `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("feature")
}
`)
	runGit(t, workRoot, "commit", "-am", "feature")
	headSHA := strings.TrimSpace(runGit(t, workRoot, "rev-parse", "HEAD"))

	runGit(t, workRoot, "checkout", "main")
	writeTestFile(t, workRoot, "README.md", "base branch moved\n")
	runGit(t, workRoot, "add", "README.md")
	runGit(t, workRoot, "commit", "-m", "move base")
	baseSHA := strings.TrimSpace(runGit(t, workRoot, "rev-parse", "HEAD"))

	runGit(t, workRoot, "init", "--bare", bareRoot)
	runGit(t, workRoot, "remote", "add", "origin", bareRoot)
	runGit(t, workRoot, "push", "origin", "main", "feature")
	runGit(t, root, "clone", "--single-branch", "--branch", "feature", "file://"+bareRoot, cloneRoot)
	_, err := execGit(t.Context(), cloneRoot, "cat-file", "-e", baseSHA+"^{commit}")
	require.Error(t, err)

	req := runRequest{
		RepoRoot: cloneRoot,
		Range:    diffRange{BaseSHA: baseSHA, HeadSHA: headSHA},
		Prepare: []fetchSpec{
			{Remote: "origin", Ref: remoteTrackingFetchRef(defaultDispatchBaseRef)},
			{Remote: "origin", Ref: baseSHA},
		},
	}
	err = ensureRangeAvailable(t.Context(), &req, execGit, execGitFetch)
	require.NoError(t, err)
	changed := strings.TrimSpace(runGit(t, cloneRoot, "diff", "--name-only", baseSHA+"..."+headSHA))
	require.Equal(t, "pkg/sample_test.go", changed)
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available on PATH: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s failed: %s", strings.Join(args, " "), string(output))
	return string(output)
}
