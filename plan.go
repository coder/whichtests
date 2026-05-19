package main

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/xerrors"
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
	Notes   []string
}

type summaryEntry struct {
	Label     string
	Files     []string
	Tests     []string
	RunAll    bool
	TestCount string
	Notes     []string
}

type buildResult struct {
	Matrix  matrixOutput
	Summary summaryReport
}

type executionAccumulator struct {
	Package   string
	Files     map[string]struct{}
	Tests     map[string]struct{}
	Broadened bool
	RunAll    bool
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
		if err := selectChange(ctx, cfg, git, cache, selections, change); err != nil {
			return nil, buildResult{}, err
		}
	}

	result, err := buildExecutionPlan(selections)
	if err != nil {
		return nil, buildResult{}, err
	}
	return changedFiles, result, nil
}

func mergePackageSelection(selections map[packageKey]*packageSelection, selection *packageSelection) {
	merged := selections[selection.Key]
	if merged == nil {
		merged = &packageSelection{
			Key:   selection.Key,
			Tests: map[string]struct{}{},
			Files: map[string]struct{}{},
		}
		selections[selection.Key] = merged
	}
	merged.Broadened = merged.Broadened || selection.Broadened
	maps.Copy(merged.Files, selection.Files)
	maps.Copy(merged.Tests, selection.Tests)
}

func buildExecutionPlan(selections map[packageKey]*packageSelection) (buildResult, error) {
	accumulators := map[string]*executionAccumulator{}
	for key, selection := range selections {
		packagePath := packagePattern(key.Dir)
		if !safePackagePatternRE.MatchString(packagePath) {
			return buildResult{}, xerrors.Errorf("unsafe package path %q", packagePath)
		}
		entry := accumulators[packagePath]
		if entry == nil {
			entry = &executionAccumulator{
				Package:   packagePath,
				Files:     map[string]struct{}{},
				Tests:     map[string]struct{}{},
				TestCount: defaultTargetCount,
			}
			accumulators[packagePath] = entry
		}
		entry.Broadened = entry.Broadened || selection.Broadened
		maps.Copy(entry.Files, selection.Files)
		maps.Copy(entry.Tests, selection.Tests)
	}

	orderedPackages := slices.Sorted(maps.Keys(accumulators))
	result := buildResult{Matrix: matrixOutput{Include: []matrixEntry{}}}
	for _, packagePath := range orderedPackages {
		entry := accumulators[packagePath]
		tests := slices.Sorted(maps.Keys(entry.Tests))
		files := slices.Sorted(maps.Keys(entry.Files))
		if entry.Broadened && len(tests) > maxBroadenedTests {
			entry.RunAll = true
			entry.TestCount = runOnceTargetCount
			entry.Notes = appendUniqueNote(entry.Notes, fmt.Sprintf("Package-wide broadening selected %d tests, above the %d-test cap, so this target will run all tests once.", len(tests), maxBroadenedTests))
		}
		if unsafeTestNames := unsafeRunRegexTestNames(tests); len(unsafeTestNames) > 0 {
			entry.RunAll = true
			entry.TestCount = runOnceTargetCount
			entry.Notes = appendUniqueNote(entry.Notes, fmt.Sprintf("Selected %d test names that cannot be passed safely through RUN, so this target will run all tests once.", len(unsafeTestNames)))
		}
		runRegex := ""
		if !entry.RunAll {
			var err error
			runRegex, err = buildRunRegex(tests)
			if err != nil {
				return buildResult{}, xerrors.Errorf("build run regex for %s: %w", packagePath, err)
			}
		}
		result.Matrix.Include = append(result.Matrix.Include, matrixEntry{
			Package:   packagePath,
			RunRegex:  runRegex,
			TestCount: entry.TestCount,
		})
		result.Summary.Entries = append(result.Summary.Entries, summaryEntry{
			Label:     packagePath,
			Files:     files,
			Tests:     tests,
			RunAll:    entry.RunAll,
			TestCount: entry.TestCount,
			Notes:     entry.Notes,
		})
	}

	if len(result.Matrix.Include) > maxMatrixEntries {
		keep := maxMatrixEntries - 1
		overflowPackages := make([]string, 0, len(result.Matrix.Include)-keep)
		overflowFiles := map[string]struct{}{}
		for _, entry := range result.Matrix.Include[keep:] {
			overflowPackages = append(overflowPackages, entry.Package)
		}
		for _, entry := range result.Summary.Entries[keep:] {
			for _, filePath := range entry.Files {
				overflowFiles[filePath] = struct{}{}
			}
		}
		note := fmt.Sprintf("Matrix target cap %d hit. Collapsed %d additional packages into one overflow target that runs once.", maxMatrixEntries, len(overflowPackages))
		result.Matrix.Include = result.Matrix.Include[:keep]
		result.Matrix.Include = append(result.Matrix.Include, matrixEntry{
			Package:   strings.Join(overflowPackages, " "),
			TestCount: runOnceTargetCount,
		})
		result.Summary.Entries = result.Summary.Entries[:keep]
		result.Summary.Entries = append(result.Summary.Entries, summaryEntry{
			Label:     fmt.Sprintf("overflow target (%d packages)", len(overflowPackages)),
			Files:     slices.Sorted(maps.Keys(overflowFiles)),
			RunAll:    true,
			TestCount: runOnceTargetCount,
			Notes: []string{
				note,
				summarizePackages(overflowPackages),
			},
		})
		result.Summary.Notes = appendUniqueNote(result.Summary.Notes, note)
	}

	return result, nil
}

func summarizePackages(packages []string) string {
	display := packages
	if len(display) > maxOverflowSummaries {
		display = display[:maxOverflowSummaries]
	}
	quoted := make([]string, 0, len(display))
	for _, packagePath := range display {
		quoted = append(quoted, "`"+packagePath+"`")
	}
	note := "Packages: " + strings.Join(quoted, ", ")
	if len(packages) > len(display) {
		note += fmt.Sprintf(", and %d more.", len(packages)-len(display))
	}
	return note
}

func appendUniqueNote(notes []string, note string) []string {
	if note == "" || slices.Contains(notes, note) {
		return notes
	}
	return append(notes, note)
}

func unsafeRunRegexTestNames(tests []string) []string {
	unsafeNames := make([]string, 0)
	for _, testName := range tests {
		if !safeTestNameRE.MatchString(testName) {
			unsafeNames = append(unsafeNames, testName)
		}
	}
	return unsafeNames
}

func buildRunRegex(tests []string) (string, error) {
	quoted := make([]string, 0, len(tests))
	for _, testName := range tests {
		if !safeTestNameRE.MatchString(testName) {
			return "", xerrors.Errorf("unsafe test name %q", testName)
		}
		quoted = append(quoted, regexp.QuoteMeta(testName))
	}
	return "^(" + strings.Join(quoted, "|") + ")(/.*)?$", nil
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
			_, _ = builder.WriteString("- `" + filePath + "`\n")
		}
		return builder.String()
	}

	totalTests := 0
	for _, entry := range summary.Entries {
		totalTests += len(entry.Tests)
	}
	_, _ = fmt.Fprintf(&builder, "Selected %d tests across %d package targets.\n\n", totalTests, len(summary.Entries))
	if len(summary.Notes) > 0 {
		_, _ = builder.WriteString("Notes:\n")
		for _, note := range summary.Notes {
			_, _ = builder.WriteString("- " + note + "\n")
		}
		_, _ = builder.WriteString("\n")
	}
	for _, entry := range summary.Entries {
		_, _ = builder.WriteString("### `" + entry.Label + "`\n\n")
		_, _ = builder.WriteString("Files:\n")
		for _, filePath := range entry.Files {
			_, _ = builder.WriteString("- `" + filePath + "`\n")
		}
		if len(entry.Notes) > 0 {
			_, _ = builder.WriteString("\nNotes:\n")
			for _, note := range entry.Notes {
				_, _ = builder.WriteString("- " + note + "\n")
			}
		}
		if entry.RunAll {
			_, _ = builder.WriteString("\nRuns all tests in this target " + countDescription(entry.TestCount) + ".\n")
			if len(entry.Tests) > 0 {
				_, _ = builder.WriteString("\nAttributed tests:\n")
				for _, testName := range entry.Tests {
					_, _ = builder.WriteString("- `" + testName + "`\n")
				}
			}
			_, _ = builder.WriteString("\n")
			continue
		}
		_, _ = builder.WriteString("\nTests:\n")
		for _, testName := range entry.Tests {
			_, _ = builder.WriteString("- `" + testName + "`\n")
		}
		_, _ = builder.WriteString("\n")
	}
	return builder.String()
}

func countDescription(count string) string {
	if count == "1" {
		return "once"
	}
	return count + " times"
}

func packagePattern(dir string) string {
	cleanDir := filepath.ToSlash(filepath.Clean(dir))
	if cleanDir == "." {
		return "."
	}
	return "./" + cleanDir
}
