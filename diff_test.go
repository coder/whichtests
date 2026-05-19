package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"
)

func TestParseChangeKindAcceptsTypeChanges(t *testing.T) {
	t.Parallel()

	kind, err := parseChangeKind("T")
	require.NoError(t, err)
	require.Equal(t, changeType, kind)
}

func TestParseDiffHunks(t *testing.T) {
	t.Parallel()

	hunks, err := parseDiffHunks(strings.Join([]string{
		"@@ -10 +12 @@",
		"@@ -0,0 +5,3 @@",
		"@@ -20,4 +30,6 @@",
		"@@ malformed @@",
	}, "\n"))
	require.NoError(t, err)
	require.Equal(t, []diffHunk{
		{Old: lineRange{Start: 10, End: 10}, New: lineRange{Start: 12, End: 12}},
		{Old: lineRange{Start: 1, End: 0}, New: lineRange{Start: 5, End: 7}},
		{Old: lineRange{Start: 20, End: 23}, New: lineRange{Start: 30, End: 35}},
	}, hunks)
}

func TestParseNonNegativeInt(t *testing.T) {
	t.Parallel()

	value, err := parseNonNegativeInt("0")
	require.NoError(t, err)
	require.Zero(t, value)

	value, err = parseNonNegativeInt("42")
	require.NoError(t, err)
	require.Equal(t, 42, value)

	_, err = parseNonNegativeInt("x")
	require.Error(t, err)
}

func TestReadFileAtRevisionPropagatesExistenceCheckFailures(t *testing.T) {
	t.Parallel()

	repo := fakeGitRepo{
		revisions: map[string]map[string]string{
			"head": {
				"pkg/sample_test.go": `package sample
`,
			},
		},
		failures: map[string]gitResponse{
			gitKey("ls-tree", "-z", "--name-only", "head", "--", "pkg/sample_test.go"): {
				result: gitResult{Stderr: "fatal: ls-tree failed", ExitCode: 128},
				err:    xerrors.New("fatal: ls-tree failed"),
			},
		},
	}
	_, _, err := readFileAtRevision(t.Context(), config{RepoRoot: t.TempDir()}, repo.runner(t), "head", "pkg/sample_test.go")
	require.ErrorContains(t, err, "check whether pkg/sample_test.go exists at head")
}
