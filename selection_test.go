package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectTestsForSnapshots(t *testing.T) {
	t.Parallel()

	const changedPath = "pkg/changed_test.go"
	change := testFileChange{Kind: changeModified, OldPath: changedPath, NewPath: changedPath}

	tests := []struct {
		name              string
		oldData           []byte
		newData           []byte
		inventory         packageInventory
		hunks             []diffHunk
		wantTests         []string
		wantBroadened     bool
		wantDirectoryWide bool
		wantNoSelection   bool
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
			inventory: mustPackageInventory(t, map[string]string{
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
			inventory: mustPackageInventory(t, map[string]string{
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
			inventory: mustPackageInventory(t, map[string]string{
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
			inventory: mustPackageInventory(t, map[string]string{
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
			inventory: mustPackageInventory(t, map[string]string{
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
			inventory: mustPackageInventory(t, map[string]string{
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
			inventory: mustPackageInventory(t, map[string]string{
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
			inventory: mustPackageInventory(t, map[string]string{
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
			wantDirectoryWide: true,
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
			inventory: mustPackageInventory(t, map[string]string{
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
			wantDirectoryWide: true,
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
			inventory: mustPackageInventory(t, map[string]string{
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
			inventory: mustPackageInventory(t, map[string]string{
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
			inventory: mustPackageInventory(t, map[string]string{
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

			oldSnapshot := mustOptionalFileSnapshot(t, tt.oldData)
			newSnapshot := mustFileSnapshot(t, tt.newData)
			selection := selectTestsFromHunks(change, oldSnapshot, newSnapshot, tt.inventory, tt.hunks)
			if tt.wantNoSelection {
				require.Nil(t, selection)
				return
			}
			require.NotNil(t, selection)
			require.Equal(t, tt.wantDirectoryWide, selection.DirectoryWide)
			if tt.wantDirectoryWide {
				require.Empty(t, selection.Tests)
				require.Contains(t, selection.Files, changedPath)
			} else {
				require.Equal(t, tt.wantTests, selectionNames(selection))
			}
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
	inventory := mustPackageInventory(t, map[string]string{
		"pkg/changed_test.go": string(newData),
		"pkg/sibling_test.go": `package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`,
	})
	oldSnapshot := mustOptionalFileSnapshot(t, oldData)
	newSnapshot := mustFileSnapshot(t, newData)
	selection := selectTestsFromHunks(change, oldSnapshot, newSnapshot, inventory, []diffHunk{{
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
			inventory := mustPackageInventory(t, map[string]string{
				"pkg/changed_test.go": string(newData),
			})
			oldSnapshot := mustOptionalFileSnapshot(t, oldData)
			newSnapshot := mustFileSnapshot(t, newData)
			selection := selectTestsFromHunks(change, oldSnapshot, newSnapshot, inventory, []diffHunk{{
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
	inventory := mustPackageInventory(t, map[string]string{
		"pkg/changed_test.go": string(newData),
		"pkg/sibling_test.go": `package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`,
	})
	oldSnapshot := mustOptionalFileSnapshot(t, oldData)
	newSnapshot := mustFileSnapshot(t, newData)
	selection := selectTestsFromHunks(change, oldSnapshot, newSnapshot, inventory, []diffHunk{{
		Old: emptyRangeAt(3),
		New: singleLineRange(t, string(newData), `_ "example.com/sideeffect"`),
	}})
	require.NotNil(t, selection)
	require.Equal(t, []string{"TestAlpha", "TestBeta"}, selectionNames(selection))
	require.True(t, selection.Broadened)
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

func TestSelectChangeRequiresOldFileWhenKindExpectsIt(t *testing.T) {
	t.Parallel()

	oldPath := "pkg/old_test.go"
	newPath := "pkg/new_test.go"
	newContent := `package sample

import "testing"

func TestAlpha(t *testing.T) {}
`
	tests := []struct {
		name   string
		change testFileChange
		key    string
	}{
		{
			name:   "modified",
			change: testFileChange{Kind: changeModified, OldPath: oldPath, NewPath: oldPath},
			key:    oldPath,
		},
		{
			name:   "renamed",
			change: testFileChange{Kind: changeRenamed, OldPath: oldPath, NewPath: newPath},
			key:    oldPath + "\x00" + newPath,
		},
		{
			name:   "deleted",
			change: testFileChange{Kind: changeDeleted, OldPath: oldPath},
			key:    oldPath,
		},
		{
			name:   "type",
			change: testFileChange{Kind: changeType, OldPath: oldPath, NewPath: oldPath},
			key:    oldPath,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			headFiles := map[string]string{}
			if tt.change.NewPath != "" {
				headFiles[tt.change.NewPath] = newContent
			}
			repo := fakeGitRepo{
				revisions: map[string]map[string]string{
					"base": {},
					"head": headFiles,
				},
				diffOutputs: map[string]string{
					tt.key: diffForChange(lineRange{Start: 5, End: 5}, lineRange{Start: 5, End: 5}),
				},
			}
			cache := newInventoryCache(config{RepoRoot: "/repo", BaseSHA: "base", HeadSHA: "head"}, repo.runner(t))
			err := selectChange(t.Context(), cache, map[packageKey]*packageSelection{}, tt.change)
			require.ErrorContains(t, err, "base revision base is missing "+oldPath)
		})
	}
}
