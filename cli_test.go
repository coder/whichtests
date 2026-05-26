package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunValidationErrors(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	neverGit := func(_ context.Context, _ string, _ ...string) (gitResult, error) {
		return gitResult{}, errors.New("git should not be called")
	}

	err := runCommand(t.Context(), commandConfig{config: config{OutMatrix: "matrix.json"}}, &stdout, &stderr, neverGit, nil)
	require.EqualError(t, err, "--base-sha is required")

	err = runCommand(t.Context(), commandConfig{config: config{BaseSHA: "base"}}, &stdout, &stderr, neverGit, nil)
	require.EqualError(t, err, "--out-matrix is required")

	err = runCommand(t.Context(), commandConfig{config: config{BaseSHA: "-bad", OutMatrix: "matrix.json"}}, &stdout, &stderr, neverGit, nil)
	require.ErrorContains(t, err, "must not start with '-'")

	err = runCommand(t.Context(), commandConfig{config: config{BaseSHA: "base:bad", OutMatrix: "matrix.json"}}, &stdout, &stderr, neverGit, nil)
	require.ErrorContains(t, err, "must not contain ':'")

	err = runCommand(t.Context(), commandConfig{config: config{BaseSHA: "base\x00bad", OutMatrix: "matrix.json"}}, &stdout, &stderr, neverGit, nil)
	require.ErrorContains(t, err, "must not contain NUL bytes")
}

func TestRunWritesMatrixAndSummaryWithPackageScopedEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	baseFiles := map[string]string{
		"pkgone/shared_test.go": `package one

import "testing"

func TestShared(t *testing.T) {
	t.Log("before one")
}
`,
		"pkgtwo/shared_test.go": `package two

import "testing"

func TestShared(t *testing.T) {
	t.Log("before two")
}
`,
	}
	headFiles := map[string]string{
		"pkgone/shared_test.go": `package one

import "testing"

func TestShared(t *testing.T) {
	t.Log("changed one")
}
`,
		"pkgtwo/shared_test.go": `package two

import "testing"

func TestShared(t *testing.T) {
	t.Log("changed two")
}
`,
	}
	repo := fakeGitRepo{
		changes: []testFileChange{
			{Kind: changeModified, OldPath: "pkgone/shared_test.go", NewPath: "pkgone/shared_test.go"},
			{Kind: changeModified, OldPath: "pkgtwo/shared_test.go", NewPath: "pkgtwo/shared_test.go"},
		},
		revisions: map[string]map[string]string{"base": baseFiles, "head": headFiles},
		diffOutputs: map[string]string{
			"pkgone/shared_test.go": diffForChange(
				singleLineRange(t, baseFiles["pkgone/shared_test.go"], `t.Log("before one")`),
				singleLineRange(t, headFiles["pkgone/shared_test.go"], `t.Log("changed one")`),
			),
			"pkgtwo/shared_test.go": diffForChange(
				singleLineRange(t, baseFiles["pkgtwo/shared_test.go"], `t.Log("before two")`),
				singleLineRange(t, headFiles["pkgtwo/shared_test.go"], `t.Log("changed two")`),
			),
		},
	}

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	summaryPath := filepath.Join(repoRoot, "summary.md")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runCommand(t.Context(), commandConfig{config: config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath, OutSummary: summaryPath}}, &stdout, &stderr, repo.runner(t), nil)
	require.NoError(t, err)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "selected 2 package targets")

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 2)
	require.Equal(t, "./pkgone", matrix.Include[0].Package)
	require.Equal(t, "^(TestShared)(/.*)?$", matrix.Include[0].RunRegex)
	require.Equal(t, "10", matrix.Include[0].TestCount)
	require.Equal(t, "./pkgtwo", matrix.Include[1].Package)

	summary, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.Contains(t, string(summary), "Selected 2 tests across 2 package targets")
	require.Contains(t, string(summary), "### `./pkgone`")
	require.Contains(t, string(summary), "### `./pkgtwo`")
}

func TestRunCoalesceMergesPackagesIntoSingleMatrixRow(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	baseFiles := map[string]string{
		"pkgone/shared_test.go": `package one

import "testing"

func TestSharedOne(t *testing.T) {
	t.Log("before one")
}
`,
		"pkgtwo/shared_test.go": `package two

import "testing"

func TestSharedTwo(t *testing.T) {
	t.Log("before two")
}
`,
	}
	headFiles := map[string]string{
		"pkgone/shared_test.go": `package one

import "testing"

func TestSharedOne(t *testing.T) {
	t.Log("changed one")
}
`,
		"pkgtwo/shared_test.go": `package two

import "testing"

func TestSharedTwo(t *testing.T) {
	t.Log("changed two")
}
`,
	}
	repo := fakeGitRepo{
		changes: []testFileChange{
			{Kind: changeModified, OldPath: "pkgone/shared_test.go", NewPath: "pkgone/shared_test.go"},
			{Kind: changeModified, OldPath: "pkgtwo/shared_test.go", NewPath: "pkgtwo/shared_test.go"},
		},
		revisions: map[string]map[string]string{"base": baseFiles, "head": headFiles},
		diffOutputs: map[string]string{
			"pkgone/shared_test.go": diffForChange(
				singleLineRange(t, baseFiles["pkgone/shared_test.go"], `t.Log("before one")`),
				singleLineRange(t, headFiles["pkgone/shared_test.go"], `t.Log("changed one")`),
			),
			"pkgtwo/shared_test.go": diffForChange(
				singleLineRange(t, baseFiles["pkgtwo/shared_test.go"], `t.Log("before two")`),
				singleLineRange(t, headFiles["pkgtwo/shared_test.go"], `t.Log("changed two")`),
			),
		},
	}

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	summaryPath := filepath.Join(repoRoot, "summary.md")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cfg := commandConfig{config: config{
		RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head",
		OutMatrix: matrixPath, OutSummary: summaryPath, Coalesce: true,
	}}
	err := runCommand(t.Context(), cfg, &stdout, &stderr, repo.runner(t), nil)
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 1)
	require.Equal(t, "./pkgone ./pkgtwo", matrix.Include[0].Package)
	require.Equal(t, "^(TestSharedOne|TestSharedTwo)(/.*)?$", matrix.Include[0].RunRegex)
	require.Equal(t, "10", matrix.Include[0].TestCount)

	// Summary still shows both packages so humans see the provenance.
	summary, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.Contains(t, string(summary), "### `./pkgone`")
	require.Contains(t, string(summary), "### `./pkgtwo`")
}

func TestRunWritesSummaryToStdout(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	baseFiles := map[string]string{
		"pkg/sample_test.go": `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("before")
}
`,
	}
	headFiles := map[string]string{
		"pkg/sample_test.go": `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("after")
}
`,
	}
	repo := fakeGitRepo{
		changes:   []testFileChange{{Kind: changeModified, OldPath: "pkg/sample_test.go", NewPath: "pkg/sample_test.go"}},
		revisions: map[string]map[string]string{"base": baseFiles, "head": headFiles},
		diffOutputs: map[string]string{
			"pkg/sample_test.go": diffForChange(
				singleLineRange(t, baseFiles["pkg/sample_test.go"], `t.Log("before")`),
				singleLineRange(t, headFiles["pkg/sample_test.go"], `t.Log("after")`),
			),
		},
	}

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runCommand(t.Context(), commandConfig{config: config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath, OutSummary: "-"}}, &stdout, &stderr, repo.runner(t), nil)
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "## Go test flake detector selection")
	require.Contains(t, stdout.String(), "### `./pkg`")
	require.Contains(t, stderr.String(), "selected 1 package targets")
}

func TestRunIgnoresTestMainChanges(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	baseFiles := map[string]string{
		"pkg/setup_test.go": `package sample

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
`,
		"pkg/internal_test.go": `package sample

import "testing"

func TestInternal(t *testing.T) {
	t.Log("internal")
}
`,
		"pkg/external_test.go": `package sample_test

import "testing"

func TestExternal(t *testing.T) {
	t.Log("external")
}
`,
	}
	headFiles := map[string]string{
		"pkg/setup_test.go": `package sample

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	fmt.Println("setup")
	os.Exit(m.Run())
}
`,
		"pkg/internal_test.go": baseFiles["pkg/internal_test.go"],
		"pkg/external_test.go": baseFiles["pkg/external_test.go"],
	}
	repo := fakeGitRepo{
		changes:   []testFileChange{{Kind: changeModified, OldPath: "pkg/setup_test.go", NewPath: "pkg/setup_test.go"}},
		revisions: map[string]map[string]string{"base": baseFiles, "head": headFiles},
		diffOutputs: map[string]string{
			"pkg/setup_test.go": diffForChange(
				singleLineRange(t, baseFiles["pkg/setup_test.go"], `os.Exit(m.Run())`),
				singleLineRange(t, headFiles["pkg/setup_test.go"], `fmt.Println("setup")`),
			),
		},
	}

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	summaryPath := filepath.Join(repoRoot, "summary.md")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runCommand(t.Context(), commandConfig{config: config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath, OutSummary: summaryPath}}, &stdout, &stderr, repo.runner(t), nil)
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Empty(t, matrix.Include)

	summary, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.Contains(t, string(summary), "no runnable top-level tests were selected")
	require.NotContains(t, string(summary), "TestInternal")
	require.NotContains(t, string(summary), "TestExternal")
}

func TestRunHandlesRename(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	oldPath := "pkg/old_test.go"
	newPath := "pkg/new_test.go"
	baseFiles := map[string]string{
		oldPath: `package sample

import "testing"

func TestRenamed(t *testing.T) {
	t.Log("before rename")
}
`,
	}
	headFiles := map[string]string{
		newPath: `package sample

import "testing"

func TestRenamed(t *testing.T) {
	t.Log("after rename")
}
`,
	}
	repo := fakeGitRepo{
		changes:   []testFileChange{{Kind: changeRenamed, OldPath: oldPath, NewPath: newPath}},
		revisions: map[string]map[string]string{"base": baseFiles, "head": headFiles},
		diffOutputs: map[string]string{
			oldPath + "\x00" + newPath: diffForChange(
				singleLineRange(t, baseFiles[oldPath], `t.Log("before rename")`),
				singleLineRange(t, headFiles[newPath], `t.Log("after rename")`),
			),
		},
	}

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	summaryPath := filepath.Join(repoRoot, "summary.md")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runCommand(t.Context(), commandConfig{config: config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath, OutSummary: summaryPath}}, &stdout, &stderr, repo.runner(t), nil)
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 1)
	require.Equal(t, "./pkg", matrix.Include[0].Package)
	require.Equal(t, "^(TestRenamed)(/.*)?$", matrix.Include[0].RunRegex)

	summary, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.Contains(t, string(summary), newPath)
}

func TestRunUsesHeadRevisionInsteadOfWorkingTree(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, repoRoot, "pkg/sample_test.go", `package sample

import "testing"

func TestWorkingTree(t *testing.T) {
	t.Log("working tree")
}
`)

	baseFiles := map[string]string{
		"pkg/sample_test.go": `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`,
	}
	headFiles := map[string]string{
		"pkg/sample_test.go": `package sample

import "testing"

func TestHead(t *testing.T) {
	t.Log("head")
}
`,
	}
	repo := fakeGitRepo{
		changes:   []testFileChange{{Kind: changeModified, OldPath: "pkg/sample_test.go", NewPath: "pkg/sample_test.go"}},
		revisions: map[string]map[string]string{"base": baseFiles, "head": headFiles},
		diffOutputs: map[string]string{
			"pkg/sample_test.go": diffForChange(
				singleLineRange(t, baseFiles["pkg/sample_test.go"], `func TestAlpha`),
				singleLineRange(t, headFiles["pkg/sample_test.go"], `func TestHead`),
			),
		},
	}

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runCommand(t.Context(), commandConfig{config: config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath}}, &stdout, &stderr, repo.runner(t), nil)
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 1)
	require.Equal(t, "^(TestHead)(/.*)?$", matrix.Include[0].RunRegex)
	require.NotContains(t, string(matrixData), "TestWorkingTree")
}

func TestRunSkipsNonRunnableChangedTestFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	headFiles := map[string]string{
		"pkg/testdata/example_test.go": `package sample

import "testing"

func TestIgnored(t *testing.T) {
	t.Log("ignored")
}
`,
		"pkg/_ignored_test.go": `package sample

import "testing"

func TestUnderscoreIgnored(t *testing.T) {
	t.Log("ignored")
}
`,
		"pkg/.hidden_test.go": `package sample

import "testing"

func TestHiddenIgnored(t *testing.T) {
	t.Log("ignored")
}
`,
	}
	repo := fakeGitRepo{
		changes: []testFileChange{
			{Kind: changeAdded, NewPath: "pkg/testdata/example_test.go"},
			{Kind: changeAdded, NewPath: "pkg/_ignored_test.go"},
			{Kind: changeAdded, NewPath: "pkg/.hidden_test.go"},
		},
		revisions:   map[string]map[string]string{"base": {}, "head": headFiles},
		diffOutputs: map[string]string{},
	}

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	summaryPath := filepath.Join(repoRoot, "summary.md")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runCommand(t.Context(), commandConfig{config: config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath, OutSummary: summaryPath}}, &stdout, &stderr, repo.runner(t), nil)
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Empty(t, matrix.Include)

	summary, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.Contains(t, string(summary), "No changed `*_test.go` files were detected")
}

func TestRunToleratesDuplicateRunnableNamesInPackageInventory(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	linuxPath := "pkg/platform_linux_test.go"
	windowsPath := "pkg/platform_windows_test.go"
	baseFiles := map[string]string{
		linuxPath: `//go:build linux

package sample

import "testing"

func TestPlatform(t *testing.T) {
	t.Log("linux before")
}
`,
		windowsPath: `//go:build windows

package sample

import "testing"

func TestPlatform(t *testing.T) {
	t.Log("windows")
}
`,
	}
	headFiles := map[string]string{
		linuxPath: `//go:build linux

package sample

import "testing"

func TestPlatform(t *testing.T) {
	t.Log("linux after")
}
`,
		windowsPath: baseFiles[windowsPath],
	}
	repo := fakeGitRepo{
		changes:   []testFileChange{{Kind: changeModified, OldPath: linuxPath, NewPath: linuxPath}},
		revisions: map[string]map[string]string{"base": baseFiles, "head": headFiles},
		diffOutputs: map[string]string{
			linuxPath: diffForChange(
				singleLineRange(t, baseFiles[linuxPath], `t.Log("linux before")`),
				singleLineRange(t, headFiles[linuxPath], `t.Log("linux after")`),
			),
		},
	}

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runCommand(t.Context(), commandConfig{config: config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath}}, &stdout, &stderr, repo.runner(t), nil)
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 1)
	require.Equal(t, "^(TestPlatform)(/.*)?$", matrix.Include[0].RunRegex)
}

func TestRunIgnoresDeletedSetupFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	setupPath := "pkg/setup_test.go"
	testPath := "pkg/alpha_test.go"
	baseFiles := map[string]string{
		setupPath: `package sample

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("setup")
}
`,
		testPath: `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`,
	}
	headFiles := map[string]string{
		testPath: baseFiles[testPath],
	}
	repo := fakeGitRepo{
		changes:   []testFileChange{{Kind: changeDeleted, OldPath: setupPath}},
		revisions: map[string]map[string]string{"base": baseFiles, "head": headFiles},
		diffOutputs: map[string]string{
			setupPath: diffForChange(
				singleLineRange(t, baseFiles[setupPath], `t.Log("setup")`),
				emptyRangeAt(1),
			),
		},
	}

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	summaryPath := filepath.Join(repoRoot, "summary.md")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runCommand(t.Context(), commandConfig{config: config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath, OutSummary: summaryPath}}, &stdout, &stderr, repo.runner(t), nil)
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Empty(t, matrix.Include)

	summary, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.Contains(t, string(summary), setupPath)
	require.Contains(t, string(summary), "no runnable top-level tests were selected")
	require.NotContains(t, string(summary), "TestAlpha")
}

func TestRunIgnoresInitChanges(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	setupPath := "pkg/external_setup_test.go"
	baseFiles := map[string]string{
		setupPath: `package sample_test

func init() {
	println("before")
}
`,
		"pkg/internal_test.go": `package sample

import "testing"

func TestInternal(t *testing.T) {
	t.Log("internal")
}
`,
		"pkg/external_test.go": `package sample_test

import "testing"

func TestExternal(t *testing.T) {
	t.Log("external")
}
`,
	}
	headFiles := map[string]string{
		setupPath: `package sample_test

func init() {
	println("after")
}
`,
		"pkg/internal_test.go": baseFiles["pkg/internal_test.go"],
		"pkg/external_test.go": baseFiles["pkg/external_test.go"],
	}
	repo := fakeGitRepo{
		changes:   []testFileChange{{Kind: changeModified, OldPath: setupPath, NewPath: setupPath}},
		revisions: map[string]map[string]string{"base": baseFiles, "head": headFiles},
		diffOutputs: map[string]string{
			setupPath: diffForChange(
				singleLineRange(t, baseFiles[setupPath], `println("before")`),
				singleLineRange(t, headFiles[setupPath], `println("after")`),
			),
		},
	}

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runCommand(t.Context(), commandConfig{config: config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath}}, &stdout, &stderr, repo.runner(t), nil)
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Empty(t, matrix.Include)
}

func TestRunHandlesCrossDirectoryRenamePrecisely(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	oldPath := "oldpkg/moved_test.go"
	newPath := "newpkg/moved_test.go"
	baseFiles := map[string]string{
		oldPath: `package oldpkg

import "testing"

func TestMoved(t *testing.T) {
	t.Log("before")
}
`,
		"oldpkg/stable_test.go": `package oldpkg

import "testing"

func TestOldStable(t *testing.T) {
	t.Log("old")
}
`,
	}
	headFiles := map[string]string{
		newPath: `package newpkg

import "testing"

func TestMoved(t *testing.T) {
	t.Log("after")
}
`,
		"oldpkg/stable_test.go": baseFiles["oldpkg/stable_test.go"],
	}
	repo := fakeGitRepo{
		changes:   []testFileChange{{Kind: changeRenamed, OldPath: oldPath, NewPath: newPath}},
		revisions: map[string]map[string]string{"base": baseFiles, "head": headFiles},
		diffOutputs: map[string]string{
			oldPath + "\x00" + newPath: diffForChange(
				singleLineRange(t, baseFiles[oldPath], `t.Log("before")`),
				singleLineRange(t, headFiles[newPath], `t.Log("after")`),
			),
		},
	}

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runCommand(t.Context(), commandConfig{config: config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath}}, &stdout, &stderr, repo.runner(t), nil)
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 1)
	require.Equal(t, "./newpkg", matrix.Include[0].Package)
	require.Equal(t, "^(TestMoved)(/.*)?$", matrix.Include[0].RunRegex)
}

func TestRunIgnoresCrossDirectoryHelperRenameSource(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	oldPath := "oldpkg/setup_test.go"
	newPath := "newpkg/setup_test.go"
	baseFiles := map[string]string{
		oldPath: `package oldpkg

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("before")
}
`,
		"oldpkg/stable_test.go": `package oldpkg

import "testing"

func TestOldStable(t *testing.T) {
	t.Log("old")
}
`,
	}
	headFiles := map[string]string{
		newPath: `package newpkg

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("after")
}
`,
		"oldpkg/stable_test.go": baseFiles["oldpkg/stable_test.go"],
		"newpkg/stable_test.go": `package newpkg

import "testing"

func TestNewStable(t *testing.T) {
	t.Log("new")
}
`,
	}
	repo := fakeGitRepo{
		changes:   []testFileChange{{Kind: changeRenamed, OldPath: oldPath, NewPath: newPath}},
		revisions: map[string]map[string]string{"base": baseFiles, "head": headFiles},
		diffOutputs: map[string]string{
			oldPath + "\x00" + newPath: diffForChange(
				singleLineRange(t, baseFiles[oldPath], `t.Log("before")`),
				singleLineRange(t, headFiles[newPath], `t.Log("after")`),
			),
		},
	}

	matrixPath := filepath.Join(repoRoot, "matrix.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runCommand(t.Context(), commandConfig{config: config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath}}, &stdout, &stderr, repo.runner(t), nil)
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Empty(t, matrix.Include)
}
