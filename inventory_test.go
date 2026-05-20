package main

import (
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
