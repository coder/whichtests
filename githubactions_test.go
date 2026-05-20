package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitHubActionsRunRequestPullRequest(t *testing.T) {
	eventPath := writeGitHubEvent(t, `{
		"pull_request": {
			"base": {
				"sha": "base123",
				"ref": "main",
				"repo": {"full_name": "coder/coder"}
			},
			"head": {"sha": "head123"}
		},
		"ignored": true
	}`)
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
	t.Setenv("GITHUB_OUTPUT", "output.txt")
	t.Setenv("GITHUB_STEP_SUMMARY", "summary.md")
	t.Setenv("UNRELATED_EXTRA_ENV", "ignored")

	req, err := githubActionsRunRequest(t.Context(), commandConfig{
		config: config{RepoRoot: "/repo", OutMatrix: "matrix.json"},
	}, fakeGitRepo{headSHA: "head123"}.runner(t))
	require.NoError(t, err)
	require.Equal(t, "/repo", req.RepoRoot)
	require.Equal(t, diffRange{BaseSHA: "base123", HeadSHA: "head123"}, req.Range)
	require.Equal(t, []fetchSpec{
		{Remote: "https://github.com/coder/coder.git", Ref: "refs/heads/main"},
		{Remote: "https://github.com/coder/coder.git", Ref: "base123"},
	}, req.Fetches)
	require.Equal(t, "matrix.json", req.Sinks.OutMatrix)
	require.Equal(t, "output.txt", req.Sinks.GitHubOutput)
	require.Equal(t, "summary.md", req.Sinks.GitHubStepSummary)
}

func TestGitHubActionsRunRequestVerifiesPullRequestHead(t *testing.T) {
	eventPath := writeGitHubEvent(t, `{
		"pull_request": {
			"base": {
				"sha": "base123",
				"ref": "main",
				"repo": {"full_name": "coder/coder"}
			},
			"head": {"sha": "expected-head"}
		}
	}`)
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
	t.Setenv("GITHUB_OUTPUT", "output.txt")

	_, err := githubActionsRunRequest(t.Context(), commandConfig{
		config: config{RepoRoot: "/repo", OutMatrix: "matrix.json"},
	}, fakeGitRepo{headSHA: "actual-head"}.runner(t))
	require.ErrorContains(t, err, "checked out HEAD actual-head does not match pull_request.head.sha expected-head")
}

func TestGitHubActionsRunRequestRequiresPullRequestHead(t *testing.T) {
	eventPath := writeGitHubEvent(t, `{
		"pull_request": {
			"base": {
				"sha": "base123",
				"ref": "main",
				"repo": {"full_name": "coder/coder"}
			},
			"head": {"sha": ""}
		}
	}`)
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
	t.Setenv("GITHUB_OUTPUT", "output.txt")

	_, err := githubActionsRunRequest(t.Context(), commandConfig{
		config: config{RepoRoot: "/repo", OutMatrix: "matrix.json"},
	}, fakeGitRepo{headSHA: "head123"}.runner(t))
	require.ErrorContains(t, err, "pull_request.head.sha is required")
}

func TestGitHubActionsRunRequestWorkflowDispatchExplicitRange(t *testing.T) {
	eventPath := writeGitHubEvent(t, `{
		"inputs": {
			"base_sha": "base123",
			"head_sha": "head123"
		}
	}`)
	t.Setenv("GITHUB_EVENT_NAME", "workflow_dispatch")
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
	t.Setenv("GITHUB_OUTPUT", "output.txt")

	req, err := githubActionsRunRequest(t.Context(), commandConfig{
		config: config{RepoRoot: "/repo", OutMatrix: "matrix.json"},
	}, fakeGitRepo{headSHA: "head123"}.runner(t))
	require.NoError(t, err)
	require.Equal(t, diffRange{BaseSHA: "base123", HeadSHA: "head123"}, req.Range)
	require.Equal(t, []fetchSpec{
		{Remote: "origin", Ref: "refs/heads/main:refs/remotes/origin/main"},
		{Remote: "origin", Ref: "base123"},
	}, req.Fetches)
	require.Empty(t, req.MergeBaseRef)
}

func TestRunCommandGitHubActionsWritesOutputs(t *testing.T) {
	oldContent := `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("old")
}
`
	newContent := `package sample

import "testing"

func TestAlpha(t *testing.T) {
	t.Log("new")
}
`
	rangeForAlpha := singleLineRange(t, newContent, `t.Log("new")`)
	repo := fakeGitRepo{
		headSHA: "head123",
		changes: []testFileChange{{
			Kind:    changeModified,
			OldPath: "pkg/sample_test.go",
			NewPath: "pkg/sample_test.go",
		}},
		revisions: map[string]map[string]string{
			"base123": {"pkg/sample_test.go": oldContent},
			"head123": {"pkg/sample_test.go": newContent},
		},
		diffOutputs: map[string]string{
			"pkg/sample_test.go": diffForChange(rangeForAlpha, rangeForAlpha),
		},
	}
	tmpDir := t.TempDir()
	eventPath := writeGitHubEvent(t, `{
		"pull_request": {
			"base": {
				"sha": "base123",
				"ref": "main",
				"repo": {"full_name": "coder/coder"}
			},
			"head": {"sha": "head123"}
		}
	}`)
	outputPath := filepath.Join(tmpDir, "github-output.txt")
	stepSummaryPath := filepath.Join(tmpDir, "step-summary.md")
	localSummaryPath := filepath.Join(tmpDir, "summary.md")
	matrixPath := filepath.Join(tmpDir, "matrix.json")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
	t.Setenv("GITHUB_OUTPUT", outputPath)
	t.Setenv("GITHUB_STEP_SUMMARY", stepSummaryPath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	fetch := func(context.Context, string, fetchSpec) (gitResult, error) {
		t.Fatal("unexpected fetch")
		return gitResult{}, nil
	}
	err := runCommand(t.Context(), commandConfig{
		config:        config{RepoRoot: "/repo", OutMatrix: matrixPath, OutSummary: localSummaryPath},
		GitHubActions: true,
	}, &stdout, &stderr, repo.runner(t), fetch)
	require.NoError(t, err)

	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	require.JSONEq(t, `{"include":[{"package":"./pkg","run_regex":"^(TestAlpha)(/.*)?$","test_count":"10"}]}`, string(matrixData))
	outputData, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	require.Equal(t, "matrix="+string(bytes.TrimSpace(matrixData))+"\n", string(outputData))
	stepSummary, err := os.ReadFile(stepSummaryPath)
	require.NoError(t, err)
	require.Contains(t, string(stepSummary), `"pkg/sample_test.go"`)
	require.Contains(t, string(stepSummary), "TestAlpha")
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "selected 1 package targets")
}

func TestEnsureRangeAvailableWorkflowDispatchDefaultBase(t *testing.T) {
	t.Parallel()

	req := runRequest{
		RepoRoot:     "/repo",
		Range:        diffRange{HeadSHA: "head123"},
		Fetches:      []fetchSpec{{Remote: "origin", Ref: "refs/heads/main:refs/remotes/origin/main"}},
		MergeBaseRef: "origin/main",
	}
	repo := fakeGitRepo{
		revisions: map[string]map[string]string{
			"base123": {},
			"head123": {},
		},
		mergeBases: map[string]string{
			gitKey("merge-base", "head123", "origin/main"): "base123",
		},
	}
	var fetches []fetchSpec
	fetch := func(_ context.Context, _ string, spec fetchSpec) (gitResult, error) {
		fetches = append(fetches, spec)
		return gitResult{}, nil
	}
	err := ensureRangeAvailable(t.Context(), &req, repo.runner(t), fetch)
	require.NoError(t, err)
	require.Equal(t, "base123", req.Range.BaseSHA)
	require.Equal(t, []fetchSpec{{Remote: "origin", Ref: "refs/heads/main:refs/remotes/origin/main"}}, fetches)
}

func TestEnsureRangeAvailableFetchesLazily(t *testing.T) {
	t.Parallel()

	req := runRequest{
		RepoRoot: "/repo",
		Range:    diffRange{BaseSHA: "base123", HeadSHA: "head123"},
		Fetches:  []fetchSpec{{Remote: "https://github.com/coder/coder.git", Ref: "refs/heads/main"}},
	}
	repo := fakeGitRepo{revisions: map[string]map[string]string{"base123": {}, "head123": {}}}
	fetch := func(_ context.Context, _ string, spec fetchSpec) (gitResult, error) {
		t.Fatalf("unexpected fetch: %+v", spec)
		return gitResult{}, nil
	}
	require.NoError(t, ensureRangeAvailable(t.Context(), &req, repo.runner(t), fetch))
}

func TestEnsureRangeAvailableFetchesWhenMergeBaseIsMissing(t *testing.T) {
	t.Parallel()

	req := runRequest{
		RepoRoot: "/repo",
		Range:    diffRange{BaseSHA: "base123", HeadSHA: "head123"},
		Fetches: []fetchSpec{
			{Remote: "https://github.com/coder/coder.git", Ref: "refs/heads/main"},
			{Remote: "https://github.com/coder/coder.git", Ref: "base123"},
		},
	}
	mergeBaseCalls := 0
	git := func(_ context.Context, _ string, args ...string) (gitResult, error) {
		require.Equal(t, []string{"merge-base", "base123", "head123"}, args)
		mergeBaseCalls++
		if mergeBaseCalls == 1 {
			return gitFailure(1, "fatal: no merge base")
		}
		return gitResult{Stdout: "base123\n"}, nil
	}
	var fetches []fetchSpec
	fetch := func(_ context.Context, _ string, spec fetchSpec) (gitResult, error) {
		fetches = append(fetches, spec)
		return gitResult{}, nil
	}
	require.NoError(t, ensureRangeAvailable(t.Context(), &req, git, fetch))
	require.Equal(t, 2, mergeBaseCalls)
	require.Equal(t, req.Fetches[:1], fetches)
}

func TestGitHubActionsRunRequestValidatesInputsBeforeFetch(t *testing.T) {
	tests := []struct {
		name      string
		eventName string
		eventJSON string
		want      string
	}{
		{
			name:      "bad base revision",
			eventName: "pull_request",
			eventJSON: `{"pull_request":{"base":{"sha":"-bad","ref":"main","repo":{"full_name":"coder/coder"}},"head":{"sha":"head123"}}}`,
			want:      "must not start with '-'",
		},
		{
			name:      "bad base ref",
			eventName: "pull_request",
			eventJSON: `{"pull_request":{"base":{"sha":"base123","ref":"main:evil","repo":{"full_name":"coder/coder"}},"head":{"sha":"head123"}}}`,
			want:      "safe branch ref",
		},
		{
			name:      "bad base repo",
			eventName: "pull_request",
			eventJSON: `{"pull_request":{"base":{"sha":"base123","ref":"main","repo":{"full_name":"../coder"}},"head":{"sha":"head123"}}}`,
			want:      "owner/repository",
		},
		{
			name:      "bad dispatch head",
			eventName: "workflow_dispatch",
			eventJSON: `{"inputs":{"head_sha":"head:bad"}}`,
			want:      "must not contain ':'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eventPath := writeGitHubEvent(t, tc.eventJSON)
			t.Setenv("GITHUB_EVENT_NAME", tc.eventName)
			t.Setenv("GITHUB_EVENT_PATH", eventPath)
			t.Setenv("GITHUB_OUTPUT", "output.txt")

			_, err := githubActionsRunRequest(t.Context(), commandConfig{
				config: config{RepoRoot: "/repo", OutMatrix: "matrix.json"},
			}, fakeGitRepo{headSHA: "head123"}.runner(t))
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func writeGitHubEvent(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
