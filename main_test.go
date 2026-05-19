package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"
)

func TestSelectTestsForSnapshots(t *testing.T) {
	t.Parallel()

	const changedPath = "pkg/changed_test.go"
	change := testFileChange{Kind: changeModified, OldPath: changedPath, NewPath: changedPath}

	tests := []struct {
		name            string
		oldData         []byte
		newData         []byte
		inventory       packageInventory
		hunks           []diffHunk
		wantTests       []string
		wantBroadened   bool
		wantNoSelection bool
	}{
		{
			name: "body change selects only changed test",
			oldData: []byte(`package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("before alpha")
}

func TestBeta(t *testing.T) {
	t.Log("stable beta")
}
`),
			newData: []byte(`package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("changed alpha")
}

func TestBeta(t *testing.T) {
	t.Log("stable beta")
}
`),
			inventory: mustPackageInventory(t, "pkg", "sample", map[string]string{
				changedPath: `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("changed alpha")
}

func TestBeta(t *testing.T) {
	t.Log("stable beta")
}
`,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("before alpha")
}

func TestBeta(t *testing.T) {
	t.Log("stable beta")
}
`, `t.Log("before alpha")`),
				New: singleLineRange(t, `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("changed alpha")
}

func TestBeta(t *testing.T) {
	t.Log("stable beta")
}
`, `t.Log("changed alpha")`),
			}},
			wantTests: []string{"TestAlpha"},
		},
		{
			name: "new top-level test selects only new test",
			oldData: []byte(`package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`),
			newData: []byte(`package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func TestBeta(t *testing.T) {
	t.Log("new beta")
}
`),
			inventory: mustPackageInventory(t, "pkg", "sample", map[string]string{
				changedPath: `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func TestBeta(t *testing.T) {
	t.Log("new beta")
}
`,
			}),
			hunks: []diffHunk{{
				Old: emptyRangeAt(7),
				New: singleLineRange(t, `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func TestBeta(t *testing.T) {
	t.Log("new beta")
}
`, `t.Log("new beta")`),
			}},
			wantTests: []string{"TestBeta"},
		},
		{
			name: "existing helper change broadens across package",
			oldData: []byte(`package sample

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("before helper")
}

func TestAlpha(t *testing.T) {
	setup(t)
}
`),
			newData: []byte(`package sample

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("changed helper")
}

func TestAlpha(t *testing.T) {
	setup(t)
}
`),
			inventory: mustPackageInventory(t, "pkg", "sample", map[string]string{
				changedPath: `package sample

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("changed helper")
}

func TestAlpha(t *testing.T) {
	setup(t)
}
`,
				"pkg/sibling_test.go": `package sample

import "testing"

func TestBeta(t *testing.T) {
	setup(t)
}
`,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, `package sample

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("before helper")
}

func TestAlpha(t *testing.T) {
	setup(t)
}
`, `t.Log("before helper")`),
				New: singleLineRange(t, `package sample

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("changed helper")
}

func TestAlpha(t *testing.T) {
	setup(t)
}
`, `t.Log("changed helper")`),
			}},
			wantTests:     []string{"TestAlpha", "TestBeta"},
			wantBroadened: true,
		},
		{
			name: "package variable change broadens across package",
			oldData: []byte(`package sample

import "testing"

var packageValue = 1

func TestAlpha(t *testing.T) {
	t.Log(packageValue)
}
`),
			newData: []byte(`package sample

import "testing"

var packageValue = 2

func TestAlpha(t *testing.T) {
	t.Log(packageValue)
}
`),
			inventory: mustPackageInventory(t, "pkg", "sample", map[string]string{
				changedPath: `package sample

import "testing"

var packageValue = 2

func TestAlpha(t *testing.T) {
	t.Log(packageValue)
}
`,
				"pkg/sibling_test.go": `package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log(packageValue)
}
`,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, `package sample

import "testing"

var packageValue = 1

func TestAlpha(t *testing.T) {
	t.Log(packageValue)
}
`, "var packageValue = 1"),
				New: singleLineRange(t, `package sample

import "testing"

var packageValue = 2

func TestAlpha(t *testing.T) {
	t.Log(packageValue)
}
`, "var packageValue = 2"),
			}},
			wantTests:     []string{"TestAlpha", "TestBeta"},
			wantBroadened: true,
		},
		{
			name: "additive import broadens package",
			oldData: []byte(`package sample

import (
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`),
			newData: []byte(`package sample

import (
	"fmt"
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`),
			inventory: mustPackageInventory(t, "pkg", "sample", map[string]string{
				changedPath: `package sample

import (
	"fmt"
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`,
			}),
			hunks: []diffHunk{{
				Old: emptyRangeAt(singleLineRange(t, `package sample

import (
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`, `"testing"`).Start),
				New: singleLineRange(t, `package sample

import (
	"fmt"
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`, `"fmt"`),
			}},
			wantTests:     []string{"TestAlpha", "TestBeta"},
			wantBroadened: true,
		},
		{
			name: "additive helper with new test stays narrow",
			oldData: []byte(`package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`),
			newData: []byte(`package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func setupCase(t *testing.T) {
	t.Helper()
	t.Log("beta helper")
}

func TestBeta(t *testing.T) {
	setupCase(t)
}
`),
			inventory: mustPackageInventory(t, "pkg", "sample", map[string]string{
				changedPath: `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func setupCase(t *testing.T) {
	t.Helper()
	t.Log("beta helper")
}

func TestBeta(t *testing.T) {
	setupCase(t)
}
`,
			}),
			hunks: []diffHunk{{
				Old: emptyRangeAt(7),
				New: rangeSpan(
					singleLineRange(t, `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func setupCase(t *testing.T) {
	t.Helper()
	t.Log("beta helper")
}

func TestBeta(t *testing.T) {
	setupCase(t)
}
`, "func setupCase(t *testing.T) {"),
					singleLineRange(t, `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func setupCase(t *testing.T) {
	t.Helper()
	t.Log("beta helper")
}

func TestBeta(t *testing.T) {
	setupCase(t)
}
`, "setupCase(t)"),
				),
			}},
			wantTests: []string{"TestBeta"},
		},
		{
			name: "removed import broadens across package",
			oldData: []byte(`package sample

import (
	"fmt"
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`),
			newData: []byte(`package sample

import (
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`),
			inventory: mustPackageInventory(t, "pkg", "sample", map[string]string{
				changedPath: `package sample

import (
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`,
				"pkg/sibling_test.go": `package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, `package sample

import (
	"fmt"
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`, `"fmt"`),
				New: emptyRangeAt(singleLineRange(t, `package sample

import (
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`, `"testing"`).Start),
			}},
			wantTests:     []string{"TestAlpha", "TestBeta"},
			wantBroadened: true,
		},
		{
			name: "TestMain broadens across sibling files in same package",
			oldData: []byte(`package sample

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
`),
			newData: []byte(`package sample

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	fmt.Println("setup")
	os.Exit(m.Run())
}
`),
			inventory: mustPackageInventory(t, "pkg", "sample", map[string]string{
				changedPath: `package sample

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
				"pkg/internal_test.go": `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, `package sample

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
`, `os.Exit(m.Run())`),
				New: singleLineRange(t, `package sample

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	fmt.Println("setup")
	os.Exit(m.Run())
}
`, `fmt.Println("setup")`),
			}},
			wantTests:     []string{"TestAlpha"},
			wantBroadened: true,
		},
		{
			name: "init broadens across sibling files in same package",
			oldData: []byte(`package sample

import "testing"

func init() {
	register("before")
}
`),
			newData: []byte(`package sample

import "testing"

func init() {
	register("after")
}
`),
			inventory: mustPackageInventory(t, "pkg", "sample", map[string]string{
				changedPath: `package sample

import "testing"

func init() {
	register("after")
}
`,
				"pkg/internal_test.go": `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, `package sample

import "testing"

func init() {
	register("before")
}
`, `register("before")`),
				New: singleLineRange(t, `package sample

import "testing"

func init() {
	register("after")
}
`, `register("after")`),
			}},
			wantTests:     []string{"TestAlpha"},
			wantBroadened: true,
		},
		{
			name: "malformed changed file broadens package conservatively",
			oldData: []byte(`package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("before alpha")
}
`),
			newData: []byte(`package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("changed alpha")

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`),
			inventory: mustPackageInventory(t, "pkg", "sample", map[string]string{
				changedPath: `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("changed alpha")

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`,
				"pkg/sibling_test.go": `package sample

import "testing"

func TestGamma(t *testing.T) {
	t.Log("gamma")
}
`,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("before alpha")
}
`, `t.Log("before alpha")`),
				New: singleLineRange(t, `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("changed alpha")

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`, `t.Log("changed alpha")`),
			}},
			wantTests:     []string{"TestAlpha", "TestBeta", "TestGamma"},
			wantBroadened: true,
		},
		{
			name: "deleted helper uses old snapshot to broaden package",
			oldData: []byte(`package sample

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("helper")
}

func TestAlpha(t *testing.T) {
	setup(t)
}
`),
			newData: []byte(`package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`),
			inventory: mustPackageInventory(t, "pkg", "sample", map[string]string{
				changedPath: `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`,
				"pkg/sibling_test.go": `package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`,
			}),
			hunks: []diffHunk{{
				Old: rangeSpan(
					singleLineRange(t, `package sample

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("helper")
}

func TestAlpha(t *testing.T) {
	setup(t)
}
`, "func setup(t *testing.T) {"),
					singleLineRange(t, `package sample

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("helper")
}

func TestAlpha(t *testing.T) {
	setup(t)
}
`, `t.Log("helper")`),
				),
				New: emptyRangeAt(singleLineRange(t, `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`, `func TestAlpha(t *testing.T) {`).Start),
			}},
			wantTests:     []string{"TestAlpha", "TestBeta"},
			wantBroadened: true,
		},
		{
			name:    "brand-new file with additive hunk selects only new tests",
			oldData: nil,
			newData: []byte(`package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log("new beta")
}
`),
			inventory: mustPackageInventory(t, "pkg", "sample", map[string]string{
				changedPath: `package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log("new beta")
}
`,
			}),
			hunks: []diffHunk{{
				Old: emptyRangeAt(1),
				New: rangeSpan(
					singleLineRange(t, `package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log("new beta")
}
`, "func TestBeta(t *testing.T) {"),
					singleLineRange(t, `package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log("new beta")
}
`, `t.Log("new beta")`),
				),
			}},
			wantTests: []string{"TestBeta"},
		},
		{
			name: "dot imported testing is recognized",
			oldData: []byte(`package sample

import . "testing"

func TestAlpha(t *T) {
	t.Log("before alpha")
}
`),
			newData: []byte(`package sample

import . "testing"

func TestAlpha(t *T) {
	t.Log("changed alpha")
}
`),
			inventory: mustPackageInventory(t, "pkg", "sample", map[string]string{
				changedPath: `package sample

import . "testing"

func TestAlpha(t *T) {
	t.Log("changed alpha")
}
`,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, `package sample

import . "testing"

func TestAlpha(t *T) {
	t.Log("before alpha")
}
`, `t.Log("before alpha")`),
				New: singleLineRange(t, `package sample

import . "testing"

func TestAlpha(t *T) {
	t.Log("changed alpha")
}
`, `t.Log("changed alpha")`),
			}},
			wantTests: []string{"TestAlpha"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			selection := selectTestsForSnapshots(change, tt.oldData, tt.newData, tt.inventory, tt.hunks)
			if tt.wantNoSelection {
				require.Nil(t, selection)
				return
			}
			require.NotNil(t, selection)
			require.Equal(t, tt.wantTests, selectionNames(selection))
			require.Equal(t, tt.wantBroadened, selection.Broadened)
		})
	}
}

func TestSelectTestsForSnapshotsTreatsTestMethodsAsSharedHelpers(t *testing.T) {
	t.Parallel()

	change := testFileChange{Kind: changeModified, OldPath: "pkg/changed_test.go", NewPath: "pkg/changed_test.go"}
	oldData := []byte(`package sample

import "testing"

type suite struct{}

func (suite) TestMethod(t *testing.T) {
	t.Log("before method")
}

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`)
	newData := []byte(`package sample

import "testing"

type suite struct{}

func (suite) TestMethod(t *testing.T) {
	t.Log("changed method")
}

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`)
	inventory := mustPackageInventory(t, "pkg", "sample", map[string]string{
		"pkg/changed_test.go": string(newData),
		"pkg/sibling_test.go": `package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`,
	})
	selection := selectTestsForSnapshots(change, oldData, newData, inventory, []diffHunk{{
		Old: singleLineRange(t, string(oldData), `t.Log("before method")`),
		New: singleLineRange(t, string(newData), `t.Log("changed method")`),
	}})
	require.NotNil(t, selection)
	require.Equal(t, []string{"TestAlpha", "TestBeta"}, selectionNames(selection))
	require.True(t, selection.Broadened)
}

func TestSelectTestsForSnapshotsAdditiveSharedDeclsStayNarrow(t *testing.T) {
	t.Parallel()

	change := testFileChange{Kind: changeModified, OldPath: "pkg/changed_test.go", NewPath: "pkg/changed_test.go"}
	basePrefix := `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

`
	cases := []struct {
		name        string
		declaration string
		needle      string
	}{
		{name: "var", declaration: "var packageValue = 1\n", needle: "var packageValue = 1"},
		{name: "const", declaration: "const packageValue = 1\n", needle: "const packageValue = 1"},
		{name: "type", declaration: "type packageValue struct{}\n", needle: "type packageValue struct{}"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			oldData := []byte(basePrefix)
			newData := []byte(basePrefix + tt.declaration + `
func TestBeta(t *testing.T) {
	t.Log("beta")
}
`)
			inventory := mustPackageInventory(t, "pkg", "sample", map[string]string{
				"pkg/changed_test.go": string(newData),
			})
			selection := selectTestsForSnapshots(change, oldData, newData, inventory, []diffHunk{{
				Old: emptyRangeAt(7),
				New: rangeSpan(
					singleLineRange(t, string(newData), tt.needle),
					singleLineRange(t, string(newData), `t.Log("beta")`),
				),
			}})
			require.NotNil(t, selection)
			require.Equal(t, []string{"TestBeta"}, selectionNames(selection))
			require.False(t, selection.Broadened)
		})
	}
}

func TestSelectTestsForSnapshotsBroadensAddedImports(t *testing.T) {
	t.Parallel()

	change := testFileChange{Kind: changeModified, OldPath: "pkg/changed_test.go", NewPath: "pkg/changed_test.go"}
	oldData := []byte(`package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`)
	newData := []byte(`package sample

import (
	_ "example.com/sideeffect"
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`)
	inventory := mustPackageInventory(t, "pkg", "sample", map[string]string{
		"pkg/changed_test.go": string(newData),
		"pkg/sibling_test.go": `package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`,
	})
	selection := selectTestsForSnapshots(change, oldData, newData, inventory, []diffHunk{{
		Old: emptyRangeAt(3),
		New: singleLineRange(t, string(newData), `_ "example.com/sideeffect"`),
	}})
	require.NotNil(t, selection)
	require.Equal(t, []string{"TestAlpha", "TestBeta"}, selectionNames(selection))
	require.True(t, selection.Broadened)
}

func TestParseFileSnapshotRejectsLowercaseSuffixes(t *testing.T) {
	t.Parallel()

	snapshot, err := parseFileSnapshot([]byte(`package sample

import "testing"

func TestAlpha(t *testing.T) {}
func Testify(t *testing.T) {}
func FuzzAlpha(f *testing.F) {}
func Fuzzbar(f *testing.F) {}
func Example() {}
func ExampleFoo() {}
func Examplefoo() {}
`))
	require.NoError(t, err)
	require.Equal(t, []string{"Example", "ExampleFoo", "FuzzAlpha", "TestAlpha"}, slices.Sorted(maps.Keys(snapshot.tests)))
}

func TestFallbackTestNamesRejectsLowercaseSuffixes(t *testing.T) {
	t.Parallel()

	data := []byte(`package sample

import "testing"

func TestAlpha(t *testing.T) {}
func Testify(t *testing.T) {}
func FuzzAlpha(f *testing.F) {}
func Fuzzbar(f *testing.F) {}
func Example() {}
func ExampleFoo() {}
func Examplefoo() {}
`)
	require.Equal(t, []string{"Example", "ExampleFoo", "FuzzAlpha", "TestAlpha"}, fallbackTestNames(data))
}

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

func TestBuildRunRegexRejectsUnsafeNames(t *testing.T) {
	t.Parallel()

	_, err := buildRunRegex([]string{"TestAlpha", "TestO'Brien"})
	require.Error(t, err)
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
	require.Equal(t, "1", overflow.TestCount)
	require.Empty(t, overflow.RunRegex)
	require.Contains(t, overflow.Package, "./pkg")
	require.Contains(t, result.Summary.Notes[0], "Matrix target cap")
	require.Contains(t, result.Summary.Entries[len(result.Summary.Entries)-1].Notes[1], "and 3 more")
}

func TestRunValidationErrors(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	neverGit := func(_ context.Context, _ string, _ ...string) (gitResult, error) {
		return gitResult{}, xerrors.New("git should not be called")
	}

	err := run(t.Context(), config{OutMatrix: "matrix.json"}, &stdout, &stderr, neverGit)
	require.EqualError(t, err, "--base-sha is required")

	err = run(t.Context(), config{BaseSHA: "base"}, &stdout, &stderr, neverGit)
	require.EqualError(t, err, "--out-matrix is required")

	err = run(t.Context(), config{BaseSHA: "-bad", OutMatrix: "matrix.json"}, &stdout, &stderr, neverGit)
	require.ErrorContains(t, err, "must not start with '-'")

	err = run(t.Context(), config{BaseSHA: "base:bad", OutMatrix: "matrix.json"}, &stdout, &stderr, neverGit)
	require.ErrorContains(t, err, "must not contain ':'")

	err = run(t.Context(), config{BaseSHA: "base\x00bad", OutMatrix: "matrix.json"}, &stdout, &stderr, neverGit)
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
	err := run(t.Context(), config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath, OutSummary: summaryPath}, &stdout, &stderr, repo.runner(t))
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
	err := run(t.Context(), config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath, OutSummary: "-"}, &stdout, &stderr, repo.runner(t))
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "## Go test flake detector selection")
	require.Contains(t, stdout.String(), "### `./pkg`")
	require.Contains(t, stderr.String(), "selected 1 package targets")
}

func TestRunBroadensTestMainAcrossPackageAndPackageTest(t *testing.T) {
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
	err := run(t.Context(), config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath, OutSummary: summaryPath}, &stdout, &stderr, repo.runner(t))
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 1)
	require.Equal(t, "./pkg", matrix.Include[0].Package)
	require.Equal(t, "^(TestExternal|TestInternal)(/.*)?$", matrix.Include[0].RunRegex)

	summary, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.Contains(t, string(summary), "TestInternal")
	require.Contains(t, string(summary), "TestExternal")
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
	err := run(t.Context(), config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath, OutSummary: summaryPath}, &stdout, &stderr, repo.runner(t))
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
	err := run(t.Context(), config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath}, &stdout, &stderr, repo.runner(t))
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
	err := run(t.Context(), config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath, OutSummary: summaryPath}, &stdout, &stderr, repo.runner(t))
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
	err := run(t.Context(), config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath}, &stdout, &stderr, repo.runner(t))
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 1)
	require.Equal(t, "^(TestPlatform)(/.*)?$", matrix.Include[0].RunRegex)
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

func TestMergePackageSelectionCombinesSamePackageFiles(t *testing.T) {
	t.Parallel()

	key := packageKey{Dir: "pkg", Name: "sample"}
	selections := map[packageKey]*packageSelection{}
	mergePackageSelection(selections, &packageSelection{
		Key:   key,
		Tests: map[string]struct{}{"TestAlpha": {}},
		Files: map[string]struct{}{"pkg/alpha_test.go": {}},
	})
	mergePackageSelection(selections, &packageSelection{
		Key:       key,
		Tests:     map[string]struct{}{"TestBeta": {}},
		Files:     map[string]struct{}{"pkg/beta_test.go": {}},
		Broadened: true,
	})

	require.Equal(t, []string{"TestAlpha", "TestBeta"}, selectionNames(selections[key]))
	require.True(t, selections[key].Broadened)
	require.Contains(t, selections[key].Files, "pkg/alpha_test.go")
	require.Contains(t, selections[key].Files, "pkg/beta_test.go")
}

func TestRunHandlesDeletedSetupFile(t *testing.T) {
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
	err := run(t.Context(), config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath, OutSummary: summaryPath}, &stdout, &stderr, repo.runner(t))
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 1)
	require.Equal(t, "^(TestAlpha)(/.*)?$", matrix.Include[0].RunRegex)

	summary, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.Contains(t, string(summary), setupPath)
	require.Contains(t, string(summary), "TestAlpha")
}

func TestRunBroadensInitAcrossPackageAndPackageTest(t *testing.T) {
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
	err := run(t.Context(), config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath}, &stdout, &stderr, repo.runner(t))
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 1)
	require.Equal(t, "^(TestExternal|TestInternal)(/.*)?$", matrix.Include[0].RunRegex)
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
	err := run(t.Context(), config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath}, &stdout, &stderr, repo.runner(t))
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 1)
	require.Equal(t, "./newpkg", matrix.Include[0].Package)
	require.Equal(t, "^(TestMoved)(/.*)?$", matrix.Include[0].RunRegex)
}

func TestRunHandlesCrossDirectoryRenameSourceFallout(t *testing.T) {
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
	err := run(t.Context(), config{RepoRoot: repoRoot, BaseSHA: "base", HeadSHA: "head", OutMatrix: matrixPath}, &stdout, &stderr, repo.runner(t))
	require.NoError(t, err)

	var matrix matrixOutput
	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(matrixData, &matrix))
	require.Len(t, matrix.Include, 1)
	require.Equal(t, "./oldpkg", matrix.Include[0].Package)
	require.Equal(t, "^(TestOldStable)(/.*)?$", matrix.Include[0].RunRegex)
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

func selectionNames(selection *packageSelection) []string {
	if selection == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(selection.Tests))
}

func mustPackageInventory(t *testing.T, dir, packageName string, files map[string]string) packageInventory {
	t.Helper()
	inventory := packageInventory{
		Key:   packageKey{Dir: dir, Name: packageName},
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

type fakeGitRepo struct {
	changes     []testFileChange
	revisions   map[string]map[string]string
	diffOutputs map[string]string
	mergeBases  map[string]string
	headSHA     string
	failures    map[string]gitResponse
}

type gitResponse struct {
	result gitResult
	err    error
}

func (repo fakeGitRepo) runner(t *testing.T) gitRunner {
	t.Helper()
	return func(_ context.Context, _ string, args ...string) (gitResult, error) {
		t.Helper()
		if response, ok := repo.failures[gitKey(args...)]; ok {
			return response.result, response.err
		}
		switch args[0] {
		case "diff":
			return repo.diffResponse(t, args)
		case "cat-file":
			return repo.catFileResponse(t, args)
		case "show":
			return repo.showResponse(t, args)
		case "ls-tree":
			return repo.lsTreeResponse(t, args)
		case "merge-base":
			return repo.mergeBaseResponse(t, args)
		case "rev-parse":
			return repo.revParseResponse(t, args)
		default:
			t.Fatalf("unexpected git command: %v", args)
			return gitResult{}, nil
		}
	}
}

func (repo fakeGitRepo) diffResponse(t *testing.T, args []string) (gitResult, error) {
	t.Helper()
	if len(args) >= 2 && args[1] == "--name-status" {
		return gitResult{Stdout: repo.nameStatusOutput()}, nil
	}
	separator := slices.Index(args, "--")
	require.NotEqual(t, -1, separator)
	paths := args[separator+1:]
	output, ok := repo.diffOutputs[strings.Join(paths, "\x00")]
	if !ok {
		t.Fatalf("unexpected diff paths %q", strings.Join(paths, "\x00"))
	}
	return gitResult{Stdout: output}, nil
}

func (repo fakeGitRepo) nameStatusOutput() string {
	parts := make([]string, 0, len(repo.changes)*3)
	for _, change := range repo.changes {
		switch change.Kind {
		case changeRenamed:
			parts = append(parts, "R100", change.OldPath, change.NewPath)
		case changeAdded:
			parts = append(parts, string(change.Kind), change.NewPath)
		case changeDeleted:
			parts = append(parts, string(change.Kind), change.OldPath)
		default:
			parts = append(parts, string(change.Kind), change.displayPath())
		}
	}
	return strings.Join(parts, "\x00") + "\x00"
}

func (repo fakeGitRepo) catFileResponse(t *testing.T, args []string) (gitResult, error) {
	t.Helper()
	require.Len(t, args, 3)
	require.Equal(t, "-e", args[1])
	spec := args[2]
	if strings.HasSuffix(spec, "^{commit}") {
		revision := strings.TrimSuffix(spec, "^{commit}")
		if _, ok := repo.revisions[revision]; ok {
			return gitResult{}, nil
		}
		return gitFailure(128, fmt.Sprintf("fatal: bad revision %q", revision))
	}
	revision, path := splitRevisionPath(t, spec)
	if _, ok := repo.revisions[revision][path]; ok {
		return gitResult{}, nil
	}
	return gitFailure(128, fmt.Sprintf("fatal: path %q does not exist in %q", path, revision))
}

func (repo fakeGitRepo) showResponse(t *testing.T, args []string) (gitResult, error) {
	t.Helper()
	require.Len(t, args, 2)
	revision, path := splitRevisionPath(t, args[1])
	content, ok := repo.revisions[revision][path]
	if !ok {
		return gitFailure(128, fmt.Sprintf("fatal: path %q does not exist in %q", path, revision))
	}
	return gitResult{Stdout: content}, nil
}

func (repo fakeGitRepo) lsTreeResponse(t *testing.T, args []string) (gitResult, error) {
	t.Helper()
	separator := slices.Index(args, "--")
	require.Greater(t, separator, 1)
	require.Less(t, separator+1, len(args))
	revision := args[separator-1]
	pathspec := cleanGitPath(args[separator+1])
	files := make([]string, 0)
	for filePath := range repo.revisions[revision] {
		cleanPath := cleanGitPath(filePath)
		if pathspec != "." && !strings.HasPrefix(cleanPath, pathspec+"/") && cleanPath != pathspec {
			continue
		}
		files = append(files, cleanPath)
	}
	slices.Sort(files)
	return gitResult{Stdout: strings.Join(files, "\x00") + "\x00"}, nil
}

func (repo fakeGitRepo) mergeBaseResponse(t *testing.T, args []string) (gitResult, error) {
	t.Helper()
	require.Len(t, args, 3)
	key := gitKey(args...)
	if repo.mergeBases != nil {
		if base, ok := repo.mergeBases[key]; ok {
			return gitResult{Stdout: base + "\n"}, nil
		}
	}
	left := args[1]
	if _, ok := repo.revisions[left]; ok {
		return gitResult{Stdout: left + "\n"}, nil
	}
	return gitFailure(1, fmt.Sprintf("fatal: no merge base for %s and %s", args[1], args[2]))
}

func (repo fakeGitRepo) revParseResponse(t *testing.T, args []string) (gitResult, error) {
	t.Helper()
	require.Equal(t, []string{"rev-parse", "HEAD"}, args)
	head := repo.headSHA
	if head == "" {
		head = "head"
	}
	return gitResult{Stdout: head + "\n"}, nil
}

func splitRevisionPath(t *testing.T, spec string) (revision string, path string) {
	t.Helper()
	revision, path, ok := strings.Cut(spec, ":")
	require.True(t, ok)
	return revision, cleanGitPath(path)
}

func gitFailure(exitCode int, stderr string) (gitResult, error) {
	return gitResult{Stderr: stderr, ExitCode: exitCode}, xerrors.New(stderr)
}

func gitKey(args ...string) string {
	// NUL is a stable separator because git diff pathspecs can contain spaces.
	return strings.Join(args, "\x00")
}
