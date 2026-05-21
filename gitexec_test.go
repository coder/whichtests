package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecGitNoMergeBaseDiagnosticIsGeneric(t *testing.T) {
	t.Parallel()

	requireGit(t)
	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test User")
	writeTestFile(t, repoRoot, "pkg/sample_test.go", `package sample
`)
	runGit(t, repoRoot, "add", ".")
	runGit(t, repoRoot, "commit", "-m", "base")
	runGit(t, repoRoot, "branch", "left")

	runGit(t, repoRoot, "checkout", "--orphan", "right")
	require.NoError(t, os.Remove(filepath.Join(repoRoot, "pkg", "sample_test.go")))
	writeTestFile(t, repoRoot, "pkg/sample_test.go", `package sample
`)
	runGit(t, repoRoot, "add", ".")
	runGit(t, repoRoot, "commit", "-m", "right")

	_, err := execGit(t.Context(), repoRoot, "diff", "left...right", "--", "pkg/sample_test.go")
	require.Error(t, err)
	require.ErrorContains(t, err, "Ensure both revisions have full history before diffing")
	require.NotContains(t, err.Error(), `diffing "pkg/sample_test.go"`)
}
