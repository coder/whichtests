package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeGitRepo struct {
	changes     []testFileChange
	revisions   map[string]map[string]string
	diffOutputs map[string]string
	mergeBases  map[string]string
	headSHA     string
	failures    map[string]gitResponse
}

type gitResponse struct {
	result gitResult
	err    error
}

func (repo fakeGitRepo) runner(t *testing.T) gitRunner {
	t.Helper()
	return func(_ context.Context, _ string, args ...string) (gitResult, error) {
		t.Helper()
		if response, ok := repo.failures[gitKey(args...)]; ok {
			return response.result, response.err
		}
		switch args[0] {
		case "diff":
			return repo.diffResponse(t, args)
		case "cat-file":
			return repo.catFileResponse(t, args)
		case "show":
			return repo.showResponse(t, args)
		case "ls-tree":
			return repo.lsTreeResponse(t, args)
		case "merge-base":
			return repo.mergeBaseResponse(t, args)
		case "rev-parse":
			return repo.revParseResponse(t, args)
		default:
			t.Fatalf("unexpected git command: %v", args)
			return gitResult{}, nil
		}
	}
}

func (repo fakeGitRepo) diffResponse(t *testing.T, args []string) (gitResult, error) {
	t.Helper()
	if len(args) >= 2 && args[1] == "--name-status" {
		return gitResult{Stdout: repo.nameStatusOutput()}, nil
	}
	separator := slices.Index(args, "--")
	require.NotEqual(t, -1, separator)
	paths := args[separator+1:]
	output, ok := repo.diffOutputs[strings.Join(paths, "\x00")]
	if !ok {
		t.Fatalf("unexpected diff paths %q", strings.Join(paths, "\x00"))
	}
	return gitResult{Stdout: output}, nil
}

func (repo fakeGitRepo) nameStatusOutput() string {
	parts := make([]string, 0, len(repo.changes)*3)
	for _, change := range repo.changes {
		switch change.Kind {
		case changeRenamed:
			parts = append(parts, "R100", change.OldPath, change.NewPath)
		case changeAdded:
			parts = append(parts, string(change.Kind), change.NewPath)
		case changeDeleted:
			parts = append(parts, string(change.Kind), change.OldPath)
		default:
			parts = append(parts, string(change.Kind), change.displayPath())
		}
	}
	return strings.Join(parts, "\x00") + "\x00"
}

func (repo fakeGitRepo) catFileResponse(t *testing.T, args []string) (gitResult, error) {
	t.Helper()
	require.Len(t, args, 3)
	require.Equal(t, "-e", args[1])
	spec := args[2]
	if revision, ok := strings.CutSuffix(spec, "^{commit}"); ok {
		if _, ok := repo.revisions[revision]; ok {
			return gitResult{}, nil
		}
		return gitFailure(128, fmt.Sprintf("fatal: bad revision %q", revision))
	}
	revision, path := splitRevisionPath(t, spec)
	if _, ok := repo.revisions[revision][path]; ok {
		return gitResult{}, nil
	}
	return gitFailure(128, fmt.Sprintf("fatal: path %q does not exist in %q", path, revision))
}

func (repo fakeGitRepo) showResponse(t *testing.T, args []string) (gitResult, error) {
	t.Helper()
	require.Len(t, args, 2)
	revision, path := splitRevisionPath(t, args[1])
	content, ok := repo.revisions[revision][path]
	if !ok {
		return gitFailure(128, fmt.Sprintf("fatal: path %q does not exist in %q", path, revision))
	}
	return gitResult{Stdout: content}, nil
}

func (repo fakeGitRepo) lsTreeResponse(t *testing.T, args []string) (gitResult, error) {
	t.Helper()
	separator := slices.Index(args, "--")
	require.Greater(t, separator, 1)
	require.Less(t, separator+1, len(args))
	revision := args[separator-1]
	pathspec := cleanGitPath(args[separator+1])
	files := make([]string, 0)
	for filePath := range repo.revisions[revision] {
		cleanPath := cleanGitPath(filePath)
		if pathspec != "." && !strings.HasPrefix(cleanPath, pathspec+"/") && cleanPath != pathspec {
			continue
		}
		files = append(files, cleanPath)
	}
	slices.Sort(files)
	return gitResult{Stdout: strings.Join(files, "\x00") + "\x00"}, nil
}

func (repo fakeGitRepo) mergeBaseResponse(t *testing.T, args []string) (gitResult, error) {
	t.Helper()
	require.Len(t, args, 3)
	key := gitKey(args...)
	if repo.mergeBases != nil {
		if base, ok := repo.mergeBases[key]; ok {
			return gitResult{Stdout: base + "\n"}, nil
		}
	}
	left := args[1]
	if _, ok := repo.revisions[left]; ok {
		return gitResult{Stdout: left + "\n"}, nil
	}
	return gitFailure(1, fmt.Sprintf("fatal: no merge base for %s and %s", args[1], args[2]))
}

func (repo fakeGitRepo) revParseResponse(t *testing.T, args []string) (gitResult, error) {
	t.Helper()
	require.Equal(t, []string{"rev-parse", "HEAD"}, args)
	head := repo.headSHA
	if head == "" {
		head = "head"
	}
	return gitResult{Stdout: head + "\n"}, nil
}

func splitRevisionPath(t *testing.T, spec string) (revision string, path string) {
	t.Helper()
	revision, path, ok := strings.Cut(spec, ":")
	require.True(t, ok)
	return revision, cleanGitPath(path)
}

func gitFailure(exitCode int, stderr string) (gitResult, error) {
	return gitResult{Stderr: stderr, ExitCode: exitCode}, errors.New(stderr)
}

func gitKey(args ...string) string {
	// NUL is a stable separator because git diff pathspecs can contain spaces.
	return strings.Join(args, "\x00")
}
