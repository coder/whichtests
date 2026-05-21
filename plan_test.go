package main

import (
	"fmt"
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

func TestBuildExecutionPlanRejectsUnsafeTestNames(t *testing.T) {
	t.Parallel()

	selection := &packageSelection{
		Key:   packageKey{Dir: "pkg", Name: "sample"},
		Tests: map[string]struct{}{"TestAlpha": {}, "TestΛ": {}},
		Files: map[string]struct{}{"pkg/sample_test.go": {}},
	}
	_, err := buildExecutionPlan(map[packageKey]*packageSelection{selection.Key: selection})
	require.ErrorContains(t, err, "cannot be passed safely through RUN")
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

func TestIsSafePackagePatternAllowsSafeNamesAndRejectsTraversal(t *testing.T) {
	t.Parallel()

	for _, packagePath := range []string{".", "./foo_bar", "./foo-bar", "./foo.bar", "./foo/bar_baz"} {
		require.True(t, isSafePackagePattern(packagePath), packagePath)
	}
	for _, packagePath := range []string{"./foo/../bar", "./..", "./foo/..", "../foo", "./foo bar"} {
		require.False(t, isSafePackagePattern(packagePath), packagePath)
	}
}

func TestBuildExecutionPlanKeepsManyExactTestsPrecise(t *testing.T) {
	t.Parallel()

	selection := &packageSelection{
		Key:   packageKey{Dir: "pkg", Name: "sample"},
		Tests: map[string]struct{}{},
		Files: map[string]struct{}{"pkg/sample_test.go": {}},
	}
	for index := range 75 {
		selection.Tests[fmt.Sprintf("Test%03d", index)] = struct{}{}
	}
	result, err := buildExecutionPlan(map[packageKey]*packageSelection{selection.Key: selection})
	require.NoError(t, err)
	require.Len(t, result.Matrix.Include, 1)
	require.Equal(t, "10", result.Matrix.Include[0].TestCount)
	require.NotEmpty(t, result.Matrix.Include[0].RunRegex)
	require.Contains(t, result.Matrix.Include[0].RunRegex, "Test000")
	require.Contains(t, result.Matrix.Include[0].RunRegex, "Test074")
	require.Empty(t, result.Summary.Entries[0].Notes)
	require.Len(t, result.Summary.Entries[0].Tests, 75)
}

func TestBuildExecutionPlanDoesNotCollapseManyMatrixTargets(t *testing.T) {
	t.Parallel()

	selections := map[packageKey]*packageSelection{}
	for index := range 33 {
		key := packageKey{Dir: fmt.Sprintf("pkg%02d", index), Name: "sample"}
		selections[key] = &packageSelection{
			Key:   key,
			Tests: map[string]struct{}{fmt.Sprintf("Test%02d", index): {}},
			Files: map[string]struct{}{fmt.Sprintf("pkg%02d/file_test.go", index): {}},
		}
	}
	result, err := buildExecutionPlan(selections)
	require.NoError(t, err)
	require.Len(t, result.Matrix.Include, 33)
	for _, entry := range result.Matrix.Include {
		require.NotEmpty(t, entry.RunRegex)
		require.Equal(t, "10", entry.TestCount)
		require.True(t, isSafePackagePattern(entry.Package), entry.Package)
	}
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
	require.Empty(t, result.Summary.Entries[0].Notes)
}
