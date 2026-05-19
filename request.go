package main

import (
	"strings"

	"golang.org/x/xerrors"
)

type diffRange struct {
	BaseSHA string
	HeadSHA string
}

type runRequest struct {
	RepoRoot        string
	Range           diffRange
	Prepare         []fetchSpec
	MergeBaseRef    string
	Sinks           outputSinks
	OutputSizeLimit int
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

func validateRevision(flagName, revision string) error {
	if revision == "" {
		return xerrors.Errorf("%s is required", flagName)
	}
	if strings.HasPrefix(revision, "-") {
		return xerrors.Errorf("%s must not start with '-': %q", flagName, revision)
	}
	if strings.Contains(revision, ":") {
		return xerrors.Errorf("%s must not contain ':': %q", flagName, revision)
	}
	if strings.ContainsRune(revision, '\x00') {
		return xerrors.Errorf("%s must not contain NUL bytes", flagName)
	}
	return nil
}

func diffRangeSpec(cfg config) string {
	return cfg.BaseSHA + "..." + cfg.HeadSHA
}
