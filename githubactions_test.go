package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitHubActionsRunRequestPullRequest(t *testing.T) {
	t.Parallel()

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
	req, err := githubActionsRunRequest(t.Context(), commandConfig{
		config: config{RepoRoot: "/repo", OutMatrix: "matrix.json"},
		Env: map[string]string{
			"GITHUB_EVENT_NAME":   "pull_request",
			"GITHUB_EVENT_PATH":   eventPath,
			"GITHUB_OUTPUT":       "output.txt",
			"GITHUB_REPOSITORY":   "coder/coder",
			"GITHUB_STEP_SUMMARY": "summary.md",
			"UNRELATED_EXTRA_ENV": "ignored",
		},
	}, fakeGitRepo{headSHA: "head123"}.runner(t))
	require.NoError(t, err)
	require.Equal(t, "/repo", req.RepoRoot)
	require.Equal(t, diffRange{BaseSHA: "base123", HeadSHA: "head123"}, req.Range)
	require.Equal(t, []fetchSpec{
		{Remote: "https://github.com/coder/coder.git", Ref: "refs/heads/main"},
		{Remote: "https://github.com/coder/coder.git", Ref: "base123"},
	}, req.Prepare)
	require.Equal(t, "matrix.json", req.Sinks.OutMatrix)
	require.Equal(t, "output.txt", req.Sinks.GitHubOutput)
	require.Equal(t, "summary.md", req.Sinks.GitHubStepSummary)
}

func TestGitHubActionsRunRequestVerifiesPullRequestHead(t *testing.T) {
	t.Parallel()

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
	_, err := githubActionsRunRequest(t.Context(), commandConfig{
		config: config{RepoRoot: "/repo", OutMatrix: "matrix.json"},
		Env: map[string]string{
			"GITHUB_EVENT_NAME": "pull_request",
			"GITHUB_EVENT_PATH": eventPath,
			"GITHUB_OUTPUT":     "output.txt",
			"GITHUB_REPOSITORY": "coder/coder",
		},
	}, fakeGitRepo{headSHA: "actual-head"}.runner(t))
	require.ErrorContains(t, err, "checked out HEAD actual-head does not match pull_request.head.sha expected-head")
}

func TestGitHubActionsRunRequestWorkflowDispatchExplicitRange(t *testing.T) {
	t.Parallel()

	eventPath := writeGitHubEvent(t, `{
		"inputs": {
			"base_sha": "base123",
			"head_sha": "head123"
		}
	}`)
	req, err := githubActionsRunRequest(t.Context(), commandConfig{
		config: config{RepoRoot: "/repo", OutMatrix: "matrix.json"},
		Env: map[string]string{
			"GITHUB_EVENT_NAME": "workflow_dispatch",
			"GITHUB_EVENT_PATH": eventPath,
			"GITHUB_OUTPUT":     "output.txt",
			"GITHUB_REPOSITORY": "coder/coder",
		},
	}, fakeGitRepo{headSHA: "head123"}.runner(t))
	require.NoError(t, err)
	require.Equal(t, diffRange{BaseSHA: "base123", HeadSHA: "head123"}, req.Range)
	require.Equal(t, []fetchSpec{
		{Remote: "origin", Ref: "refs/heads/main:refs/remotes/origin/main"},
		{Remote: "origin", Ref: "base123"},
	}, req.Prepare)
	require.Empty(t, req.MergeBaseRef)
}

func TestEnsureRangeAvailableWorkflowDispatchDefaultBase(t *testing.T) {
	t.Parallel()

	req := runRequest{
		RepoRoot:     "/repo",
		Range:        diffRange{HeadSHA: "head123"},
		Prepare:      []fetchSpec{{Remote: "origin", Ref: "refs/heads/main:refs/remotes/origin/main"}},
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
		Prepare:  []fetchSpec{{Remote: "https://github.com/coder/coder.git", Ref: "refs/heads/main"}},
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
		Prepare: []fetchSpec{
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
	require.Equal(t, req.Prepare[:1], fetches)
}

func TestGitHubActionsRunRequestValidatesInputsBeforeFetch(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			eventPath := writeGitHubEvent(t, tc.eventJSON)
			_, err := githubActionsRunRequest(t.Context(), commandConfig{
				config: config{RepoRoot: "/repo", OutMatrix: "matrix.json"},
				Env: map[string]string{
					"GITHUB_EVENT_NAME": tc.eventName,
					"GITHUB_EVENT_PATH": eventPath,
					"GITHUB_OUTPUT":     "output.txt",
					"GITHUB_REPOSITORY": "coder/coder",
				},
			}, fakeGitRepo{headSHA: "head123"}.runner(t))
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestPublishPlanWritesCompactGitHubOutputs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	matrixPath := filepath.Join(root, "matrix.json")
	summaryPath := filepath.Join(root, "summary.md")
	outputPath := filepath.Join(root, "output.txt")
	stepSummaryPath := filepath.Join(root, "step-summary.md")
	summary := "## Summary\n"
	err := publishPlan(outputSinks{
		OutMatrix:         matrixPath,
		OutSummary:        summaryPath,
		GitHubOutput:      outputPath,
		GitHubStepSummary: stepSummaryPath,
	}, matrixOutput{Include: []matrixEntry{{Package: "./pkg", RunRegex: "^(TestAlpha)(/.*)?$", TestCount: "10"}}}, summary, nil, 0)
	require.NoError(t, err)

	matrixData, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	wantMatrix := `{"include":[{"package":"./pkg","run_regex":"^(TestAlpha)(/.*)?$","test_count":"10"}]}`
	require.Equal(t, wantMatrix+"\n", string(matrixData))

	outputData, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	require.Equal(t, "matrix="+wantMatrix+"\n", string(outputData))
	outputValue := strings.TrimSuffix(strings.TrimPrefix(string(outputData), "matrix="), "\n")
	require.NotContains(t, outputValue, "\n")

	localSummary, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	require.Equal(t, summary, string(localSummary))
	stepSummary, err := os.ReadFile(stepSummaryPath)
	require.NoError(t, err)
	require.Equal(t, summary, string(stepSummary))
}

func TestPublishPlanWritesEmptyMatrixAndRejectsUnsafeOutput(t *testing.T) {
	t.Parallel()

	matrixData, err := marshalMatrix(matrixOutput{})
	require.NoError(t, err)
	require.Equal(t, `{"include":[]}`, string(matrixData))

	err = appendGitHubOutput(filepath.Join(t.TempDir(), "output.txt"), "matrix", "first\nsecond", 0)
	require.ErrorContains(t, err, "single line")

	err = appendGitHubOutput(filepath.Join(t.TempDir(), "output.txt"), "matrix", "too-long", 3)
	require.ErrorContains(t, err, "above the 3 byte limit")
}

func writeGitHubEvent(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
