package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func selectionNames(selection *packageSelection) []string {
	if selection == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(selection.Tests))
}

// mustPackageInventory builds a packageInventory for the synthetic "pkg"
// directory and "sample" package used throughout the test suite.
func mustPackageInventory(t *testing.T, files map[string]string) packageInventory {
	t.Helper()
	const packageName = "sample"
	inventory := packageInventory{
		Key:   packageKey{Dir: "pkg", Name: packageName},
		Tests: map[string][]testDecl{},
	}
	for filePath, content := range files {
		snapshot, err := parseOrFallbackSnapshot([]byte(content))
		require.NoError(t, err)
		require.Equal(t, packageName, snapshot.packageName)
		for testName, declRange := range snapshot.tests {
			inventory.Tests[testName] = append(inventory.Tests[testName], testDecl{FilePath: filePath, Range: declRange})
		}
	}
	return inventory
}

func diffForChange(oldRange, newRange lineRange) string {
	return fmt.Sprintf("@@ -%s +%s @@\n", formatDiffRange(oldRange), formatDiffRange(newRange))
}

func formatDiffRange(r lineRange) string {
	if !r.hasLines() {
		start := r.Start
		if start == 0 {
			start = 1
		}
		return fmt.Sprintf("%d,0", start)
	}
	count := r.End - r.Start + 1
	if count == 1 {
		return fmt.Sprintf("%d", r.Start)
	}
	return fmt.Sprintf("%d,%d", r.Start, count)
}

func singleLineRange(t *testing.T, content, needle string) lineRange {
	t.Helper()
	line := lineNumberForSubstring(t, content, needle)
	return lineRange{Start: line, End: line}
}

func rangeSpan(start, end lineRange) lineRange {
	return lineRange{Start: start.Start, End: end.End}
}

func emptyRangeAt(start int) lineRange {
	return lineRange{Start: start, End: start - 1}
}

func lineNumberForSubstring(t *testing.T, content, needle string) int {
	t.Helper()
	lineNumber := 0
	for index, line := range strings.Split(content, "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		if lineNumber != 0 {
			t.Fatalf("needle %q matched more than once", needle)
		}
		lineNumber = index + 1
	}
	if lineNumber == 0 {
		t.Fatalf("needle %q not found", needle)
	}
	return lineNumber
}

func writeTestFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
