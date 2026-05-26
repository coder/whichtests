package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type gitResult struct {
	Stdout string
}

type gitRunner func(ctx context.Context, dir string, args ...string) (gitResult, error)

type gitFetcher func(ctx context.Context, dir string, spec fetchSpec) (gitResult, error)

func ensureRevisionExists(ctx context.Context, cfg config, git gitRunner, revision string) error {
	_, err := git(ctx, cfg.RepoRoot, "cat-file", "-e", revision+"^{commit}")
	if err != nil {
		return fmt.Errorf("revision %s is not available: %w", revision, err)
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
	result := gitResult{Stdout: stdout.String()}
	if err == nil {
		return result, nil
	}
	message := strings.TrimSpace(stderr.String())
	if message == "" {
		message = strings.TrimSpace(result.Stdout)
	}
	if strings.Contains(message, "no merge base") {
		return result, fmt.Errorf("git %s: %s. Ensure both revisions have full history before diffing", strings.Join(args, " "), message)
	}
	if message == "" {
		message = err.Error()
	}
	return result, fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
}
