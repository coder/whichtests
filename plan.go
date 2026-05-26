package main

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	safeTestNameRE       = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	safePackagePatternRE = regexp.MustCompile(`^(?:\.|\./[A-Za-z0-9._/-]+)$`)
)

type matrixOutput struct {
	Include []matrixEntry `json:"include"`
}

type matrixEntry struct {
	Package   string `json:"package"`
	RunRegex  string `json:"run_regex,omitempty"`
	TestCount string `json:"test_count"`
}

type summaryReport struct {
	Entries []summaryEntry
}

type summaryEntry struct {
	Label     string
	Files     []string
	Tests     []string
	TestCount string
	Notes     []string
}

type buildResult struct {
	Matrix  matrixOutput
	Summary summaryReport
}

type executionAccumulator struct {
	Files     map[string]struct{}
	Tests     map[string]struct{}
	TestCount string
	Notes     []string
}

func selectTestPlan(ctx context.Context, cfg config, git gitRunner) ([]string, buildResult, error) {
	changes, err := listChangedTestFiles(ctx, cfg, git)
	if err != nil {
		return nil, buildResult{}, err
	}
	changedFiles := make([]string, 0, len(changes))
	for _, change := range changes {
		changedFiles = append(changedFiles, change.displayPath())
	}

	cache := newInventoryCache(cfg, git)
	selections := map[packageKey]*packageSelection{}
	for _, change := range changes {
		if err = selectChange(ctx, cache, selections, change); err != nil {
			return nil, buildResult{}, err
		}
	}

	result, err := buildExecutionPlan(selections, planOptions{Coalesce: cfg.Coalesce})
	if err != nil {
		return nil, buildResult{}, err
	}
	return changedFiles, result, nil
}

func buildExecutionPlan(selections map[packageKey]*packageSelection, opts planOptions) (buildResult, error) {
	accumulators := map[string]*executionAccumulator{}
	for key, selection := range selections {
		if len(selection.Tests) == 0 {
			continue
		}
		packagePath := packagePattern(key.Dir)
		if !isSafePackagePattern(packagePath) {
			return buildResult{}, fmt.Errorf("unsafe package path %q", packagePath)
		}
		entry := accumulators[packagePath]
		if entry == nil {
			entry = &executionAccumulator{
				Files:     map[string]struct{}{},
				Tests:     map[string]struct{}{},
				TestCount: defaultTestCount,
			}
			accumulators[packagePath] = entry
		}
		maps.Copy(entry.Files, selection.Files)
		maps.Copy(entry.Tests, selection.Tests)
	}

	orderedPackages := slices.Sorted(maps.Keys(accumulators))
	result := buildResult{Matrix: matrixOutput{Include: []matrixEntry{}}}
	for _, packagePath := range orderedPackages {
		entry := accumulators[packagePath]
		tests := slices.Sorted(maps.Keys(entry.Tests))
		files := slices.Sorted(maps.Keys(entry.Files))
		if unsafeTestCount := unsafeRunRegexTestCount(tests); unsafeTestCount > 0 {
			return buildResult{}, fmt.Errorf("selected %d test names for %s that cannot be passed safely through RUN", unsafeTestCount, packagePath)
		}
		result.Summary.Entries = append(result.Summary.Entries, summaryEntry{
			Label:     packagePath,
			Files:     files,
			Tests:     tests,
			TestCount: entry.TestCount,
			Notes:     entry.Notes,
		})
	}
	result.Matrix.Include = buildMatrixInclude(accumulators, orderedPackages, opts)
	return result, nil
}

// planOptions configures non-essential plan-shaping behavior. Validation and
// selection are governed by config; planOptions controls only the matrix
// output shape so that future knobs (e.g. test_count overrides) can be added
// without churning every existing call site.
type planOptions struct {
	// Coalesce collapses the per-package matrix into a single row whose package
	// list and run-regex union every selected package and test. Useful when the
	// consumer wants one `go test` invocation against all selected packages
	// (for example, to amplify scheduling contention in a flake hunt) at the
	// cost of giving up per-package precision in -run.
	Coalesce bool
}

// buildMatrixInclude renders the matrix include rows for the given
// accumulators. With opts.Coalesce, it returns at most one row whose package
// is the space-joined sorted list of every selected package path and whose
// run-regex unions every selected test name across packages. Otherwise it
// returns one row per package in orderedPackages.
//
// The coalesced row trades per-package precision in -run for a single
// invocation: a test name selected in one package will also match a
// same-named test in another listed package. Per-package safety checks have
// already run in the caller, so the joined string is composed entirely of
// vetted tokens separated by spaces.
func buildMatrixInclude(accumulators map[string]*executionAccumulator, orderedPackages []string, opts planOptions) []matrixEntry {
	if len(orderedPackages) == 0 {
		return []matrixEntry{}
	}
	if !opts.Coalesce {
		entries := make([]matrixEntry, 0, len(orderedPackages))
		for _, packagePath := range orderedPackages {
			entry := accumulators[packagePath]
			tests := slices.Sorted(maps.Keys(entry.Tests))
			entries = append(entries, matrixEntry{
				Package:   packagePath,
				RunRegex:  buildRunRegex(tests),
				TestCount: entry.TestCount,
			})
		}
		return entries
	}
	unionedTests := map[string]struct{}{}
	for _, packagePath := range orderedPackages {
		maps.Copy(unionedTests, accumulators[packagePath].Tests)
	}
	return []matrixEntry{{
		Package:   strings.Join(orderedPackages, " "),
		RunRegex:  buildRunRegex(slices.Sorted(maps.Keys(unionedTests))),
		TestCount: accumulators[orderedPackages[0]].TestCount,
	}}
}

func isSafePackagePattern(packagePath string) bool {
	if !safePackagePatternRE.MatchString(packagePath) {
		return false
	}
	if packagePath == "." {
		return true
	}
	trimmed, ok := strings.CutPrefix(packagePath, "./")
	if !ok {
		return false
	}
	for segment := range strings.SplitSeq(trimmed, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}

func unsafeRunRegexTestCount(tests []string) int {
	count := 0
	for _, testName := range tests {
		if !safeTestNameRE.MatchString(testName) {
			count++
		}
	}
	return count
}

func buildRunRegex(tests []string) string {
	quoted := make([]string, 0, len(tests))
	for _, testName := range tests {
		quoted = append(quoted, regexp.QuoteMeta(testName))
	}
	return "^(" + strings.Join(quoted, "|") + ")(/.*)?$"
}

func renderSummary(changedFiles []string, summary summaryReport) string {
	var builder strings.Builder
	_, _ = builder.WriteString("## Go test flake detector selection\n\n")
	if len(changedFiles) == 0 {
		_, _ = builder.WriteString("No changed `*_test.go` files were detected.\n")
		return builder.String()
	}
	if len(summary.Entries) == 0 {
		_, _ = builder.WriteString("Changed `*_test.go` files were detected, but no runnable top-level tests were selected.\n\n")
		_, _ = builder.WriteString("Files:\n")
		for _, filePath := range changedFiles {
			_, _ = builder.WriteString("- " + renderSummaryFilePath(filePath) + "\n")
		}
		return builder.String()
	}

	totalTests := 0
	for _, entry := range summary.Entries {
		totalTests += len(entry.Tests)
	}
	_, _ = fmt.Fprintf(&builder, "Selected %d tests across %d package targets.\n\n", totalTests, len(summary.Entries))
	for _, entry := range summary.Entries {
		_, _ = builder.WriteString("### `" + entry.Label + "`\n\n")
		_, _ = builder.WriteString("Files:\n")
		for _, filePath := range entry.Files {
			_, _ = builder.WriteString("- " + renderSummaryFilePath(filePath) + "\n")
		}
		if len(entry.Notes) > 0 {
			_, _ = builder.WriteString("\nNotes:\n")
			for _, note := range entry.Notes {
				_, _ = builder.WriteString("- " + note + "\n")
			}
		}
		_, _ = builder.WriteString("\nTests:\n")
		for _, testName := range entry.Tests {
			_, _ = builder.WriteString("- `" + testName + "`\n")
		}
		_, _ = builder.WriteString("\n")
	}
	return builder.String()
}

func renderSummaryFilePath(filePath string) string {
	return strconv.QuoteToASCII(filePath)
}
