package main

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

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
