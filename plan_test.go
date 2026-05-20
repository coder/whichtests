package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderSummaryNoChangedFiles(t *testing.T) {
	t.Parallel()

	summary := renderSummary(nil, summaryReport{})
	require.Contains(t, summary, "No changed `*_test.go` files were detected")
}

func TestRenderSummaryNoRunnableTests(t *testing.T) {
	t.Parallel()

	summary := renderSummary([]string{"pkg/changed_test.go"}, summaryReport{})
	require.Contains(t, summary, "no runnable top-level tests were selected")
	require.Contains(t, summary, "pkg/changed_test.go")
}

func TestRenderSummaryQuotesFilenames(t *testing.T) {
	t.Parallel()

	summary := renderSummary([]string{"pkg/with`tick_test.go", "pkg/with\nnewline_test.go"}, summaryReport{})
	require.Contains(t, summary, `"pkg/with`+"`"+`tick_test.go"`)
	require.Contains(t, summary, `"pkg/with\nnewline_test.go"`)
	require.NotContains(t, summary, "pkg/with\nnewline_test.go")
}

func TestBuildExecutionPlanRunsAllForUnsafeTestNames(t *testing.T) {
	t.Parallel()

	selection := &packageSelection{
		Key:   packageKey{Dir: "pkg", Name: "sample"},
		Tests: map[string]struct{}{"TestAlpha": {}, "TestΛ": {}},
		Files: map[string]struct{}{"pkg/sample_test.go": {}},
	}
	result, err := buildExecutionPlan(map[packageKey]*packageSelection{selection.Key: selection})
	require.NoError(t, err)
	require.Len(t, result.Matrix.Include, 1)
	require.Empty(t, result.Matrix.Include[0].RunRegex)
	require.Equal(t, "1", result.Matrix.Include[0].TestCount)
	require.True(t, result.Summary.Entries[0].RunAll)
	require.Contains(t, result.Summary.Entries[0].Notes[0], "cannot be passed safely")
}

func TestBuildExecutionPlanRejectsUnsafePackagePaths(t *testing.T) {
	t.Parallel()

	key := packageKey{Dir: "pkg$(echo bad)", Name: "sample"}
	_, err := buildExecutionPlan(map[packageKey]*packageSelection{
		key: {
			Key:   key,
			Tests: map[string]struct{}{"TestAlpha": {}},
			Files: map[string]struct{}{"pkg$(echo bad)/sample_test.go": {}},
		},
	})
	require.ErrorContains(t, err, "unsafe package path")
}

func TestBuildExecutionPlanRejectsPackageTraversalSegments(t *testing.T) {
	t.Parallel()

	for _, packagePath := range []string{"./foo/../bar", "./..", "./foo/.."} {
		require.False(t, isSafePackagePattern(packagePath), packagePath)
	}
}

func TestBuildExecutionPlanCapsBroadenedTarget(t *testing.T) {
	t.Parallel()

	selection := &packageSelection{
		Key:       packageKey{Dir: "pkg", Name: "sample"},
		Tests:     map[string]struct{}{},
		Files:     map[string]struct{}{"pkg/setup_test.go": {}},
		Broadened: true,
	}
	for index := range maxBroadenedTests + 1 {
		selection.Tests[fmt.Sprintf("Test%03d", index)] = struct{}{}
	}
	result, err := buildExecutionPlan(map[packageKey]*packageSelection{selection.Key: selection})
	require.NoError(t, err)
	require.Len(t, result.Matrix.Include, 1)
	require.Equal(t, "1", result.Matrix.Include[0].TestCount)
	require.Empty(t, result.Matrix.Include[0].RunRegex)
	require.True(t, result.Summary.Entries[0].RunAll)
	require.Contains(t, result.Summary.Entries[0].Notes[0], "above the 50-test cap")
}

func TestBuildExecutionPlanCapsMatrixTargets(t *testing.T) {
	t.Parallel()

	selections := map[packageKey]*packageSelection{}
	for index := range maxMatrixEntries + maxOverflowSummaries + 2 {
		key := packageKey{Dir: fmt.Sprintf("pkg%02d", index), Name: "sample"}
		selections[key] = &packageSelection{
			Key:   key,
			Tests: map[string]struct{}{fmt.Sprintf("Test%02d", index): {}},
			Files: map[string]struct{}{fmt.Sprintf("pkg%02d/file_test.go", index): {}},
		}
	}
	result, err := buildExecutionPlan(selections)
	require.NoError(t, err)
	require.Len(t, result.Matrix.Include, maxMatrixEntries)
	overflow := result.Matrix.Include[len(result.Matrix.Include)-1]
	require.Equal(t, strings.Join([]string{
		"./pkg19", "./pkg20", "./pkg21", "./pkg22", "./pkg23", "./pkg24", "./pkg25",
		"./pkg26", "./pkg27", "./pkg28", "./pkg29", "./pkg30", "./pkg31",
	}, " "), overflow.Package)
	require.Empty(t, overflow.RunRegex)
	require.Equal(t, "1", overflow.TestCount)
	for _, packagePath := range strings.Fields(overflow.Package) {
		require.True(t, isSafePackagePattern(packagePath), packagePath)
	}
	require.Contains(t, result.Summary.Notes[0], "Matrix target cap")
	require.Contains(t, result.Summary.Entries[len(result.Summary.Entries)-1].Notes[1], "and 3 more")
}

func TestBuildExecutionPlanKeepsSameNamePackageAndExternalTestsPrecise(t *testing.T) {
	t.Parallel()

	selections := map[packageKey]*packageSelection{
		{Dir: "pkg", Name: "sample"}: {
			Key:   packageKey{Dir: "pkg", Name: "sample"},
			Tests: map[string]struct{}{"TestShared": {}},
			Files: map[string]struct{}{"pkg/internal_test.go": {}},
		},
		{Dir: "pkg", Name: "sample_test"}: {
			Key:   packageKey{Dir: "pkg", Name: "sample_test"},
			Tests: map[string]struct{}{"TestShared": {}},
			Files: map[string]struct{}{"pkg/external_test.go": {}},
		},
	}
	result, err := buildExecutionPlan(selections)
	require.NoError(t, err)
	require.Len(t, result.Matrix.Include, 1)
	require.Equal(t, "./pkg", result.Matrix.Include[0].Package)
	require.Equal(t, "^(TestShared)(/.*)?$", result.Matrix.Include[0].RunRegex)
	require.Equal(t, "10", result.Matrix.Include[0].TestCount)
	require.False(t, result.Summary.Entries[0].RunAll)
	require.Empty(t, result.Summary.Entries[0].Notes)
}
