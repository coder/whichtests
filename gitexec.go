package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/xerrors"
)

type gitResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type gitRunner func(ctx context.Context, dir string, args ...string) (gitResult, error)

type gitFetcher func(ctx context.Context, dir string, spec fetchSpec) (gitResult, error)

func ensureRevisionExists(ctx context.Context, cfg config, git gitRunner, revision string) error {
	_, err := git(ctx, cfg.RepoRoot, "cat-file", "-e", revision+"^{commit}")
	if err != nil {
		return xerrors.Errorf("revision %s is not available: %w", revision, err)
	}
	return nil
}

func execGit(ctx context.Context, dir string, args ...string) (gitResult, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := gitResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}
	if err == nil {
		return result, nil
	}
	result.ExitCode = exitCode(err)
	message := strings.TrimSpace(result.Stderr)
	if message == "" {
		message = strings.TrimSpace(result.Stdout)
	}
	if strings.Contains(message, "no merge base") {
		return result, xerrors.Errorf("git %s: %s. Ensure both revisions have full history before diffing %q", strings.Join(args, " "), message, args[len(args)-1])
	}
	if message == "" {
		message = err.Error()
	}
	return result, xerrors.Errorf("git %s: %s", strings.Join(args, " "), message)
}

func exitCode(err error) int {
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode()
	}
	return -1
}
