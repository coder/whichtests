package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublishPlanWritesCompactGitHubOutputs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	matrixPath := filepath.Join(root, "matrix.json")
	summaryPath := filepath.Join(root, "summary.md")
	outputPath := filepath.Join(root, "output.txt")
	stepSummaryPath := filepath.Join(root, "step-summary.md")
	summary := "## Summary\n"
	err := publishPlan(outputSinks{
		OutMatrix:         matrixPath,
		OutSummary:        summaryPath,
		GitHubOutput:      outputPath,
		GitHubStepSummary: stepSummaryPath,
	}, matrixOutput{SelectedTestCount: 1, Include: []matrixEntry{{Package: "./pkg", RunRegex: "^(TestAlpha)(/.*)?$", TestCount: "10"}}}, summary, nil)
	require.NoError(t, err)

	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	wantMatrix := `{"selected_test_count":1,"include":[{"package":"./pkg","run_regex":"^(TestAlpha)(/.*)?$","test_count":"10"}]}`
	require.Equal(t, wantMatrix+"\n", string(matrixData))

	outputData, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	require.Equal(t, "matrix="+wantMatrix+"\n", string(outputData))
	outputValue := strings.TrimSuffix(strings.TrimPrefix(string(outputData), "matrix="), "\n")
	require.NotContains(t, outputValue, "\n")

	localSummary, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.Equal(t, summary, string(localSummary))
	stepSummary, err := os.ReadFile(stepSummaryPath)
	require.NoError(t, err)
	require.Equal(t, summary, string(stepSummary))
}

func TestPublishPlanWritesEmptyMatrixAndRejectsUnsafeOutput(t *testing.T) {
	t.Parallel()

	matrixData, err := marshalMatrix(matrixOutput{})
	require.NoError(t, err)
	require.Equal(t, `{"selected_test_count":0,"include":[]}`, string(matrixData))

	err = ensureGitHubOutputFits("matrix", "first\nsecond", defaultGitHubOutputValueLimit)
	require.ErrorContains(t, err, "single line")

	err = ensureGitHubOutputFits("matrix", "too-long", 3)
	require.ErrorContains(t, err, "above the 3 byte limit")
}
