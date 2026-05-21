package main

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadPackageInventoryReturnsParseErrors(t *testing.T) {
	t.Parallel()

	repo := fakeGitRepo{
		revisions: map[string]map[string]string{
			"head": {
				"pkg/good_test.go": `package sample

import "testing"

func TestGood(t *testing.T) {}
`,
				"pkg/broken_test.go": `package sample

import "testing"

func TestBroken(t *testing.T) {
`,
			},
		},
	}
	cache := newInventoryCache(config{RepoRoot: "/repo"}, repo.runner(t))
	_, err := cache.loadPackageInventory(t.Context(), "head", packageKey{Dir: "pkg", Name: "sample"})
	require.ErrorContains(t, err, "parse pkg/broken_test.go at head")
}

func TestLoadPackageInventoryCachesResults(t *testing.T) {
	t.Parallel()

	repo := fakeGitRepo{
		revisions: map[string]map[string]string{
			"head": {
				"pkg/alpha_test.go": `package sample

import "testing"

func TestAlpha(t *testing.T) {}
`,
			},
		},
	}
	counter := newCountingGitRunner(repo.runner(t))
	cache := newInventoryCache(config{RepoRoot: "/repo"}, counter.run)
	key := packageKey{Dir: "pkg", Name: "sample"}
	inventory, err := cache.loadPackageInventory(t.Context(), "head", key)
	require.NoError(t, err)
	require.Equal(t, []string{"TestAlpha"}, slices.Sorted(maps.Keys(inventory.Tests)))
	firstCommandCount := counter.total

	inventory, err = cache.loadPackageInventory(t.Context(), "head", key)
	require.NoError(t, err)
	require.Equal(t, []string{"TestAlpha"}, slices.Sorted(maps.Keys(inventory.Tests)))
	require.Equal(t, firstCommandCount, counter.total)
}

func TestLoadDirectoryInventoriesSharesSnapshotsAcrossPackages(t *testing.T) {
	t.Parallel()

	repo := fakeGitRepo{
		revisions: map[string]map[string]string{
			"head": {
				"pkg/internal_test.go": `package foo

import "testing"

func TestInternal(t *testing.T) {}
`,
				"pkg/external_test.go": `package foo_test

import "testing"

func TestExternal(t *testing.T) {}
`,
			},
		},
	}
	counter := newCountingGitRunner(repo.runner(t))
	cache := newInventoryCache(config{RepoRoot: "/repo"}, counter.run)
	inventories, err := cache.loadDirectoryInventories(t.Context(), "head", "pkg")
	require.NoError(t, err)
	require.Len(t, inventories, 2)
	require.Equal(t, []packageKey{
		{Dir: "pkg", Name: "foo"},
		{Dir: "pkg", Name: "foo_test"},
	}, []packageKey{inventories[0].Key, inventories[1].Key})
	require.Equal(t, 1, counter.counts["cat-file"])
	require.Equal(t, 1, counter.counts["ls-tree"])
	require.Equal(t, 2, counter.counts["show"])
	firstCommandCount := counter.total

	inventories, err = cache.loadDirectoryInventories(t.Context(), "head", "pkg")
	require.NoError(t, err)
	require.Len(t, inventories, 2)
	require.Equal(t, firstCommandCount, counter.total)
}

type countingGitRunner struct {
	runner gitRunner
	counts map[string]int
	total  int
}

func newCountingGitRunner(runner gitRunner) *countingGitRunner {
	return &countingGitRunner{runner: runner, counts: map[string]int{}}
}

func (counter *countingGitRunner) run(ctx context.Context, dir string, args ...string) (gitResult, error) {
	counter.counts[args[0]]++
	counter.total++
	return counter.runner(ctx, dir, args...)
}
