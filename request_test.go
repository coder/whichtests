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
