package main

import (
	"cmp"
)

const (
	defaultRepoRoot   = "."
	defaultHeadSHA    = "HEAD"
	defaultOutSummary = "-"
	defaultTestCount  = "10"
	runOnceTestCount  = "1"

	// Package-wide and matrix-wide caps keep the detector cheap by
	// running broad fallback targets once instead of repeatedly.
	maxMatrixEntries     = 20
	maxBroadenedTests    = 50
	maxOverflowSummaries = 10
)

type config struct {
	RepoRoot   string
	BaseSHA    string
	HeadSHA    string
	OutMatrix  string
	OutSummary string
}

func defaultConfig() config {
	return config{}.withDefaults()
}

func (cfg config) withDefaults() config {
	cfg.RepoRoot = cmp.Or(cfg.RepoRoot, defaultRepoRoot)
	cfg.HeadSHA = cmp.Or(cfg.HeadSHA, defaultHeadSHA)
	cfg.OutSummary = cmp.Or(cfg.OutSummary, defaultOutSummary)
	return cfg
}

type commandConfig struct {
	config

	GitHubActions bool
}

func defaultCommandConfig() commandConfig {
	return commandConfig{config: defaultConfig()}
}
