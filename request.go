package main

import (
	"fmt"
	"strings"
)

type diffRange struct {
	BaseSHA string
	HeadSHA string
}

type runRequest struct {
	RepoRoot     string
	Range        diffRange
	Fetches      []fetchSpec
	MergeBaseRef string
	Sinks        outputSinks
}

type fetchSpec struct {
	Remote string
	Ref    string
}

type outputSinks struct {
	OutMatrix         string
	OutSummary        string
	GitHubOutput      string
	GitHubStepSummary string
}

// validateRevisionArg rejects git revision strings that would be unsafe to pass
// as a single argv element. It is not a SHA-format validator.
func validateRevisionArg(name, revision string) error {
	if revision == "" {
		return fmt.Errorf("%s is required", name)
	}
	if strings.HasPrefix(revision, "-") {
		return fmt.Errorf("%s must not start with '-': %q", name, revision)
	}
	if strings.Contains(revision, ":") {
		return fmt.Errorf("%s must not contain ':': %q", name, revision)
	}
	if strings.ContainsRune(revision, '\x00') {
		return fmt.Errorf("%s must not contain NUL bytes", name)
	}
	return nil
}

func diffRangeSpec(cfg config) string {
	return cfg.BaseSHA + "..." + cfg.HeadSHA
}
