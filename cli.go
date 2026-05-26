// Command whichtests produces deterministic Go test plans for the
// flake-go workflow.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	cfg := defaultCommandConfig()
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flags.StringVar(&cfg.RepoRoot, "repo-root", cfg.RepoRoot, "repository root")
	flags.StringVar(&cfg.BaseSHA, "base-sha", cfg.BaseSHA, "base revision to diff against")
	flags.StringVar(&cfg.HeadSHA, "head-sha", cfg.HeadSHA, "head revision to diff against")
	flags.StringVar(&cfg.OutMatrix, "out-matrix", cfg.OutMatrix, "path to write workflow matrix JSON")
	flags.StringVar(&cfg.OutSummary, "out-summary", cfg.OutSummary, "path to write Markdown summary, or - for stdout")
	flags.BoolVar(&cfg.GitHubActions, "github-actions", cfg.GitHubActions, "read diff range and output paths from GitHub Actions environment")
	flags.BoolVar(&cfg.Coalesce, "coalesce", cfg.Coalesce, "emit a single matrix row whose package list and run-regex union every selected package and test")
	if err := flags.Parse(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := runCommand(context.Background(), cfg, os.Stdout, os.Stderr, execGit, execGitFetch); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCommand(ctx context.Context, cfg commandConfig, stdout, stderr io.Writer, git gitRunner, fetch gitFetcher) error {
	var (
		req runRequest
		err error
	)
	if cfg.GitHubActions {
		req, err = githubActionsRunRequest(ctx, cfg, git)
	} else {
		req, err = explicitRunRequest(cfg.config)
	}
	if err != nil {
		return err
	}
	return executeRunRequest(ctx, req, stdout, stderr, git, fetch)
}

func explicitRunRequest(cfg config) (runRequest, error) {
	cfg = cfg.withDefaults()
	if cfg.BaseSHA == "" {
		return runRequest{}, errors.New("--base-sha is required")
	}
	if cfg.OutMatrix == "" {
		return runRequest{}, errors.New("--out-matrix is required")
	}
	if err := validateRevisionArg("--base-sha", cfg.BaseSHA); err != nil {
		return runRequest{}, err
	}
	if err := validateRevisionArg("--head-sha", cfg.HeadSHA); err != nil {
		return runRequest{}, err
	}
	return runRequest{
		RepoRoot: cfg.RepoRoot,
		Range: diffRange{
			BaseSHA: cfg.BaseSHA,
			HeadSHA: cfg.HeadSHA,
		},
		Sinks: outputSinks{
			OutMatrix:  cfg.OutMatrix,
			OutSummary: cfg.OutSummary,
		},
		Plan: planOptions{Coalesce: cfg.Coalesce},
	}, nil
}

func executeRunRequest(ctx context.Context, req runRequest, stdout, stderr io.Writer, git gitRunner, fetch gitFetcher) error {
	if err := ensureRangeAvailable(ctx, &req, git, fetch); err != nil {
		return err
	}
	selectorCfg := config{
		RepoRoot: req.RepoRoot,
		BaseSHA:  req.Range.BaseSHA,
		HeadSHA:  req.Range.HeadSHA,
		Coalesce: req.Plan.Coalesce,
	}
	changedFiles, result, err := selectTestPlan(ctx, selectorCfg, git)
	if err != nil {
		return err
	}
	summary := renderSummary(changedFiles, result.Summary)
	if err := publishPlan(req.Sinks, result.Matrix, summary, stdout); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stderr, "selected %d package targets from %d changed test files\n", len(result.Matrix.Include), len(changedFiles))
	return nil
}
