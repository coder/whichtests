package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectTestsForSnapshots(t *testing.T) {
	t.Parallel()

	const changedPath = "pkg/changed_test.go"
	change := testFileChange{Kind: changeModified, OldPath: changedPath, NewPath: changedPath}

	const (
		// These fixtures hoist repeated row sources.
		selectionFixture01 = `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("before alpha")
}

func TestBeta(t *testing.T) {
	t.Log("stable beta")
}
`
		selectionFixture02 = `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("changed alpha")
}

func TestBeta(t *testing.T) {
	t.Log("stable beta")
}
`
		selectionFixture03 = `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`
		selectionFixture04 = `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func TestBeta(t *testing.T) {
	t.Log("new beta")
}
`
		selectionFixture05 = `package sample

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("before helper")
}

func TestAlpha(t *testing.T) {
	setup(t)
}
`
		selectionFixture06 = `package sample

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("changed helper")
}

func TestAlpha(t *testing.T) {
	setup(t)
}
`
		selectionFixture07 = `package sample

import "testing"

var packageValue = 1

func TestAlpha(t *testing.T) {
	t.Log(packageValue)
}
`
		selectionFixture08 = `package sample

import "testing"

var packageValue = 2

func TestAlpha(t *testing.T) {
	t.Log(packageValue)
}
`
		selectionFixture09 = `package sample

import (
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`
		selectionFixture10 = `package sample

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
`
		selectionFixture11 = `package sample

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
`
		selectionFixture12 = `package sample

import (
	"fmt"
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`
		selectionFixture13 = `package sample

import (
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}
`
		selectionFixture14 = `package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log("beta")
}
`
		selectionFixture15 = `package sample

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
`
		selectionFixture16 = `package sample

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	fmt.Println("setup")
	os.Exit(m.Run())
}
`
		selectionFixture17 = `package sample

import "testing"

func init() {
	register("before")
}
`
		selectionFixture18 = `package sample

import "testing"

func init() {
	register("after")
}
`
		selectionFixture19 = `package sample

import "testing"

func setup(t *testing.T) {
	t.Helper()
	t.Log("helper")
}

func TestAlpha(t *testing.T) {
	setup(t)
}
`
		selectionFixture20 = `package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log("new beta")
}
`
		selectionFixture21 = `package sample

import . "testing"

func TestAlpha(t *T) {
	t.Log("before alpha")
}
`
		selectionFixture22 = `package sample

import . "testing"

func TestAlpha(t *T) {
	t.Log("changed alpha")
}
`
	)

	tests := []struct {
		name            string
		oldData         []byte
		newData         []byte
		inventory       packageInventory
		hunks           []diffHunk
		wantTests       []string
		wantNoSelection bool
	}{
		{
			name:    "body change selects only changed test",
			oldData: []byte(selectionFixture01),
			newData: []byte(selectionFixture02),
			inventory: mustPackageInventory(t, map[string]string{
				changedPath: selectionFixture02,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, selectionFixture01, `t.Log("before alpha")`),
				New: singleLineRange(t, selectionFixture02, `t.Log("changed alpha")`),
			}},
			wantTests: []string{"TestAlpha"},
		},
		{
			name:    "new top-level test selects only new test",
			oldData: []byte(selectionFixture03),
			newData: []byte(selectionFixture04),
			inventory: mustPackageInventory(t, map[string]string{
				changedPath: selectionFixture04,
			}),
			hunks: []diffHunk{{
				Old: emptyRangeAt(7),
				New: singleLineRange(t, selectionFixture04, `t.Log("new beta")`),
			}},
			wantTests: []string{"TestBeta"},
		},
		{
			name:    "existing helper change selects no tests",
			oldData: []byte(selectionFixture05),
			newData: []byte(selectionFixture06),
			inventory: mustPackageInventory(t, map[string]string{
				changedPath: selectionFixture06,
				"pkg/sibling_test.go": `package sample

import "testing"

func TestBeta(t *testing.T) {
	setup(t)
}
`,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, selectionFixture05, `t.Log("before helper")`),
				New: singleLineRange(t, selectionFixture06, `t.Log("changed helper")`),
			}},
			wantNoSelection: true,
		},
		{
			name:    "package variable change selects no tests",
			oldData: []byte(selectionFixture07),
			newData: []byte(selectionFixture08),
			inventory: mustPackageInventory(t, map[string]string{
				changedPath: selectionFixture08,
				"pkg/sibling_test.go": `package sample

import "testing"

func TestBeta(t *testing.T) {
	t.Log(packageValue)
}
`,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, selectionFixture07, "var packageValue = 1"),
				New: singleLineRange(t, selectionFixture08, "var packageValue = 2"),
			}},
			wantNoSelection: true,
		},
		{
			name:    "additive import selects no tests",
			oldData: []byte(selectionFixture09),
			newData: []byte(selectionFixture10),
			inventory: mustPackageInventory(t, map[string]string{
				changedPath: selectionFixture10,
			}),
			hunks: []diffHunk{{
				Old: emptyRangeAt(singleLineRange(t, selectionFixture09, `"testing"`).Start),
				New: singleLineRange(t, selectionFixture10, `"fmt"`),
			}},
			wantNoSelection: true,
		},
		{
			name:    "additive helper with new test stays narrow",
			oldData: []byte(selectionFixture03),
			newData: []byte(selectionFixture11),
			inventory: mustPackageInventory(t, map[string]string{
				changedPath: selectionFixture11,
			}),
			hunks: []diffHunk{{
				Old: emptyRangeAt(7),
				New: rangeSpan(
					singleLineRange(t, selectionFixture11, "func setupCase(t *testing.T) {"),
					singleLineRange(t, selectionFixture11, "setupCase(t)"),
				),
			}},
			wantTests: []string{"TestBeta"},
		},
		{
			name:    "removed import selects no tests",
			oldData: []byte(selectionFixture12),
			newData: []byte(selectionFixture13),
			inventory: mustPackageInventory(t, map[string]string{
				changedPath:           selectionFixture13,
				"pkg/sibling_test.go": selectionFixture14,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, selectionFixture12, `"fmt"`),
				New: emptyRangeAt(singleLineRange(t, selectionFixture13, `"testing"`).Start),
			}},
			wantNoSelection: true,
		},
		{
			name:    "TestMain change selects no tests",
			oldData: []byte(selectionFixture15),
			newData: []byte(selectionFixture16),
			inventory: mustPackageInventory(t, map[string]string{
				changedPath:            selectionFixture16,
				"pkg/internal_test.go": selectionFixture03,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, selectionFixture15, `os.Exit(m.Run())`),
				New: singleLineRange(t, selectionFixture16, `fmt.Println("setup")`),
			}},
			wantNoSelection: true,
		},
		{
			name:    "init change selects no tests",
			oldData: []byte(selectionFixture17),
			newData: []byte(selectionFixture18),
			inventory: mustPackageInventory(t, map[string]string{
				changedPath:            selectionFixture18,
				"pkg/internal_test.go": selectionFixture03,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, selectionFixture17, `register("before")`),
				New: singleLineRange(t, selectionFixture18, `register("after")`),
			}},
			wantNoSelection: true,
		},
		{
			name:    "deleted helper selects no tests",
			oldData: []byte(selectionFixture19),
			newData: []byte(selectionFixture03),
			inventory: mustPackageInventory(t, map[string]string{
				changedPath:           selectionFixture03,
				"pkg/sibling_test.go": selectionFixture14,
			}),
			hunks: []diffHunk{{
				Old: rangeSpan(
					singleLineRange(t, selectionFixture19, "func setup(t *testing.T) {"),
					singleLineRange(t, selectionFixture19, `t.Log("helper")`),
				),
				New: emptyRangeAt(singleLineRange(t, selectionFixture03, `func TestAlpha(t *testing.T) {`).Start),
			}},
			wantNoSelection: true,
		},
		{
			name:    "brand-new file with additive hunk selects only new tests",
			oldData: nil,
			newData: []byte(selectionFixture20),
			inventory: mustPackageInventory(t, map[string]string{
				changedPath: selectionFixture20,
			}),
			hunks: []diffHunk{{
				Old: emptyRangeAt(1),
				New: rangeSpan(
					singleLineRange(t, selectionFixture20, "func TestBeta(t *testing.T) {"),
					singleLineRange(t, selectionFixture20, `t.Log("new beta")`),
				),
			}},
			wantTests: []string{"TestBeta"},
		},
		{
			name:    "dot imported testing is recognized",
			oldData: []byte(selectionFixture21),
			newData: []byte(selectionFixture22),
			inventory: mustPackageInventory(t, map[string]string{
				changedPath: selectionFixture22,
			}),
			hunks: []diffHunk{{
				Old: singleLineRange(t, selectionFixture21, `t.Log("before alpha")`),
				New: singleLineRange(t, selectionFixture22, `t.Log("changed alpha")`),
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
			require.Equal(t, tt.wantTests, selectionNames(selection))
		})
	}
}

func TestSelectTestsForSnapshotsIgnoresTestMethods(t *testing.T) {
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
	require.Nil(t, selection)
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
		})
	}
}

func TestSelectTestsForSnapshotsIgnoresAddedImports(t *testing.T) {
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
	require.Nil(t, selection)
}

func TestSelectTestsForSnapshotsAddedImportWithNewTestSelectsOnlyNewTest(t *testing.T) {
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
	"slices"
	"testing"
)

func TestAlpha(t *testing.T) {
	t.Log("alpha")
}

func TestBeta(t *testing.T) {
	if !slices.Contains([]string{"beta"}, "beta") {
		t.Fatal("missing beta")
	}
}
`)
	inventory := mustPackageInventory(t, map[string]string{
		"pkg/changed_test.go": string(newData),
		"pkg/sibling_test.go": `package sample

import "testing"

func TestGamma(t *testing.T) {
	t.Log("gamma")
}
`,
	})
	oldSnapshot := mustOptionalFileSnapshot(t, oldData)
	newSnapshot := mustFileSnapshot(t, newData)
	selection := selectTestsFromHunks(change, oldSnapshot, newSnapshot, inventory, []diffHunk{
		{
			Old: emptyRangeAt(3),
			New: singleLineRange(t, string(newData), `"slices"`),
		},
		{
			Old: emptyRangeAt(7),
			New: rangeSpan(
				singleLineRange(t, string(newData), "func TestBeta(t *testing.T) {"),
				singleLineRange(t, string(newData), `t.Fatal("missing beta")`),
			),
		},
	})
	require.NotNil(t, selection)
	require.Equal(t, []string{"TestBeta"}, selectionNames(selection))
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
		Key:   key,
		Tests: map[string]struct{}{"TestBeta": {}},
		Files: map[string]struct{}{"pkg/beta_test.go": {}},
	})

	require.Equal(t, []string{"TestAlpha", "TestBeta"}, selectionNames(selections[key]))
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

func TestSelectChangeRequiresNewFileWhenKindExpectsIt(t *testing.T) {
	t.Parallel()

	oldPath := "pkg/base_side_test.go"
	newPath := "pkg/head_side_test.go"
	oldContent := `package sample

import "testing"

func TestAlpha(t *testing.T) {}
`
	tests := []struct {
		name     string
		change   testFileChange
		key      string
		wantPath string
	}{
		{
			name:     "added",
			change:   testFileChange{Kind: changeAdded, NewPath: newPath},
			key:      newPath,
			wantPath: newPath,
		},
		{
			name:     "modified",
			change:   testFileChange{Kind: changeModified, OldPath: oldPath, NewPath: oldPath},
			key:      oldPath,
			wantPath: oldPath,
		},
		{
			name:     "renamed",
			change:   testFileChange{Kind: changeRenamed, OldPath: oldPath, NewPath: newPath},
			key:      oldPath + "\x00" + newPath,
			wantPath: newPath,
		},
		{
			name:     "type",
			change:   testFileChange{Kind: changeType, OldPath: oldPath, NewPath: oldPath},
			key:      oldPath,
			wantPath: oldPath,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			baseFiles := map[string]string{}
			if tt.change.OldPath != "" {
				baseFiles[tt.change.OldPath] = oldContent
			}
			repo := fakeGitRepo{
				revisions: map[string]map[string]string{
					"base": baseFiles,
					"head": {},
				},
				diffOutputs: map[string]string{
					tt.key: diffForChange(lineRange{Start: 5, End: 5}, lineRange{Start: 5, End: 5}),
				},
			}
			cache := newInventoryCache(config{RepoRoot: "/repo", BaseSHA: "base", HeadSHA: "head"}, repo.runner(t))
			err := selectChange(t.Context(), cache, map[packageKey]*packageSelection{}, tt.change)
			require.ErrorContains(t, err, "head revision head is missing "+tt.wantPath)
		})
	}
}
