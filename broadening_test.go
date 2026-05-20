package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBroadeningScopeForOldHunkChoosesMaxOverlappingScope(t *testing.T) {
	t.Parallel()

	data := []byte(`package sample

import "testing"

func init() {
	register()
}

func TestAlpha(t *testing.T) {}
`)
	snapshot, err := parseFileSnapshot(data)
	require.NoError(t, err)
	candidate := rangeSpan(
		singleLineRange(t, string(data), `import "testing"`),
		singleLineRange(t, string(data), "register()"),
	)
	require.Equal(t, broadeningDirectory, broadeningScopeForOldHunk(snapshot.shared, candidate))
}

func TestBroadeningScopeForNewHunkChoosesMaxOverlappingScope(t *testing.T) {
	t.Parallel()

	data := []byte(`package sample

import "testing"

func TestMain(m *testing.M) {
	m.Run()
}

func TestAlpha(t *testing.T) {}
`)
	snapshot, err := parseFileSnapshot(data)
	require.NoError(t, err)
	candidate := rangeSpan(
		singleLineRange(t, string(data), `import "testing"`),
		singleLineRange(t, string(data), "m.Run()"),
	)
	require.Equal(t, broadeningDirectory, broadeningScopeForNewHunk(snapshot.shared, nil, candidate))
}
