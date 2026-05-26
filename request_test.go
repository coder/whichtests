package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRevisionArgAllowsCommonGitRevisions(t *testing.T) {
	t.Parallel()

	for _, revision := range []string{
		"HEAD",
		"HEAD~3",
		"origin/main",
		"refs/heads/main",
		"v1.2.3",
		"abc1234",
		"0123456789abcdef0123456789abcdef01234567",
	} {
		require.NoError(t, validateRevisionArg("revision", revision), revision)
	}
}

func TestValidateRevisionArgRejectsUnsafeValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		revision string
		want     string
	}{
		{revision: "", want: "is required"},
		{revision: "-bad", want: "must not start with '-'"},
		{revision: "head:bad", want: "must not contain ':'"},
		{revision: "head\x00bad", want: "must not contain NUL bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			err := validateRevisionArg("--head-sha", tt.revision)
			require.ErrorContains(t, err, tt.want)
		})
	}
}
