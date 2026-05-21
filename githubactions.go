package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// defaultDispatchBaseRef follows coder/coder's default branch name.
const defaultDispatchBaseRef = "main"

var repoFullNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)

type githubEvent struct {
	PullRequest struct {
		Base struct {
			SHA  string `json:"sha"`
			Ref  string `json:"ref"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	Inputs struct {
		BaseSHA string `json:"base_sha"`
		HeadSHA string `json:"head_sha"`
	} `json:"inputs"`
}

func githubActionsRunRequest(ctx context.Context, cfg commandConfig, git gitRunner) (runRequest, error) {
	baseCfg := cfg.withDefaults()
	if baseCfg.OutMatrix == "" {
		return runRequest{}, errors.New("--out-matrix is required")
	}

	eventName := os.Getenv("GITHUB_EVENT_NAME")
	if eventName == "" {
		return runRequest{}, errors.New("GITHUB_EVENT_NAME is required")
	}
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath == "" {
		return runRequest{}, errors.New("GITHUB_EVENT_PATH is required")
	}
	githubOutput := os.Getenv("GITHUB_OUTPUT")
	if githubOutput == "" {
		return runRequest{}, errors.New("GITHUB_OUTPUT is required")
	}
	stepSummary := os.Getenv("GITHUB_STEP_SUMMARY")

	event, err := readGitHubEvent(eventPath)
	if err != nil {
		return runRequest{}, err
	}
	currentHead, err := currentHeadSHA(ctx, baseCfg.RepoRoot, git)
	if err != nil {
		return runRequest{}, err
	}

	req := runRequest{
		RepoRoot: baseCfg.RepoRoot,
		Range: diffRange{
			HeadSHA: currentHead,
		},
		Sinks: outputSinks{
			OutMatrix:         baseCfg.OutMatrix,
			OutSummary:        baseCfg.OutSummary,
			GitHubOutput:      githubOutput,
			GitHubStepSummary: stepSummary,
		},
	}

	switch eventName {
	case "pull_request":
		return pullRequestRunRequest(req, event)
	case "workflow_dispatch":
		return workflowDispatchRunRequest(req, event)
	default:
		return runRequest{}, fmt.Errorf("unsupported GitHub event %q", eventName)
	}
}

func pullRequestRunRequest(req runRequest, event githubEvent) (runRequest, error) {
	baseSHA := event.PullRequest.Base.SHA
	if err := validateRevisionArg("pull_request.base.sha", baseSHA); err != nil {
		return runRequest{}, err
	}
	baseRef := event.PullRequest.Base.Ref
	if err := validateRef("pull_request.base.ref", baseRef); err != nil {
		return runRequest{}, err
	}
	baseRepo := event.PullRequest.Base.Repo.FullName
	if err := validateRepoFullName("pull_request.base.repo.full_name", baseRepo); err != nil {
		return runRequest{}, err
	}
	payloadHead := event.PullRequest.Head.SHA
	if err := validateRevisionArg("pull_request.head.sha", payloadHead); err != nil {
		return runRequest{}, err
	}
	if req.Range.HeadSHA != payloadHead {
		return runRequest{}, fmt.Errorf("checked out HEAD %s does not match pull_request.head.sha %s; update actions/checkout ref to the pull request head commit", req.Range.HeadSHA, payloadHead)
	}

	baseURL := githubRepoURL(baseRepo)
	req.Range.BaseSHA = baseSHA
	req.Fetches = []fetchSpec{
		{Remote: baseURL, Ref: branchFetchRef(baseRef)},
		{Remote: baseURL, Ref: baseSHA},
	}
	return req, nil
}

func workflowDispatchRunRequest(req runRequest, event githubEvent) (runRequest, error) {
	if headSHA := event.Inputs.HeadSHA; headSHA != "" {
		if err := validateRevisionArg("workflow_dispatch.inputs.head_sha", headSHA); err != nil {
			return runRequest{}, err
		}
		if req.Range.HeadSHA != headSHA {
			return runRequest{}, fmt.Errorf("checked out HEAD %s does not match workflow_dispatch.inputs.head_sha %s; update actions/checkout ref to the requested head commit", req.Range.HeadSHA, headSHA)
		}
	}

	baseSHA := event.Inputs.BaseSHA
	mainFetch := fetchSpec{Remote: "origin", Ref: remoteTrackingRefspec(defaultDispatchBaseRef)}
	if baseSHA != "" {
		if err := validateRevisionArg("workflow_dispatch.inputs.base_sha", baseSHA); err != nil {
			return runRequest{}, err
		}
		req.Range.BaseSHA = baseSHA
		req.Fetches = []fetchSpec{mainFetch, {Remote: "origin", Ref: baseSHA}}
		return req, nil
	}

	req.Fetches = []fetchSpec{mainFetch}
	req.MergeBaseRef = "origin/" + defaultDispatchBaseRef
	return req, nil
}

func readGitHubEvent(path string) (githubEvent, error) {
	// #nosec G304: path comes from the GitHub Actions runner environment.
	data, err := os.ReadFile(path)
	if err != nil {
		return githubEvent{}, fmt.Errorf("read GitHub event payload %s: %w", path, err)
	}
	var event githubEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return githubEvent{}, fmt.Errorf("parse GitHub event payload %s: %w", path, err)
	}
	return event, nil
}

func currentHeadSHA(ctx context.Context, repoRoot string, git gitRunner) (string, error) {
	result, err := git(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve checked out HEAD: %w", err)
	}
	head := strings.TrimSpace(result.Stdout)
	if err := validateRevisionArg("checked out HEAD", head); err != nil {
		return "", err
	}
	return head, nil
}

func ensureRangeAvailable(ctx context.Context, req *runRequest, git gitRunner, fetch gitFetcher) error {
	if req.RepoRoot == "" {
		req.RepoRoot = defaultRepoRoot
	}
	if err := validateRevisionArg("head revision", req.Range.HeadSHA); err != nil {
		return err
	}
	if req.Range.BaseSHA != "" {
		return ensureConcreteRangeAvailable(ctx, req, git, fetch)
	}
	if req.MergeBaseRef == "" {
		return errors.New("base revision is required")
	}
	if err := runFetches(ctx, req, fetch); err != nil {
		return err
	}

	baseSHA, err := gitMergeBase(ctx, req.RepoRoot, git, req.Range.HeadSHA, req.MergeBaseRef)
	if err != nil {
		return fmt.Errorf("failed to resolve merge-base between %s and %s after fetching base history: %w", req.Range.HeadSHA, req.MergeBaseRef, err)
	}
	if err := validateRevisionArg("resolved base revision", baseSHA); err != nil {
		return err
	}
	req.Range.BaseSHA = baseSHA
	return nil
}

func ensureConcreteRangeAvailable(ctx context.Context, req *runRequest, git gitRunner, fetch gitFetcher) error {
	if err := validateRevisionArg("base revision", req.Range.BaseSHA); err != nil {
		return err
	}
	_, mergeErr := gitMergeBase(ctx, req.RepoRoot, git, req.Range.BaseSHA, req.Range.HeadSHA)
	if mergeErr == nil {
		return nil
	}
	if len(req.Fetches) == 0 {
		return fmt.Errorf("unable to resolve merge base for %s...%s: %w", req.Range.BaseSHA, req.Range.HeadSHA, mergeErr)
	}
	if fetch == nil {
		return errors.New("history fetch is required but no fetcher was configured")
	}

	attempts := []error{fmt.Errorf("initial merge-base: %w", mergeErr)}
	for _, spec := range req.Fetches {
		if err := validateFetchSpec(spec); err != nil {
			return err
		}
		if _, err := fetch(ctx, req.RepoRoot, spec); err != nil {
			attempts = append(attempts, fmt.Errorf("fetch %s from %s: %w", spec.Ref, spec.Remote, err))
			continue
		}
		_, err := gitMergeBase(ctx, req.RepoRoot, git, req.Range.BaseSHA, req.Range.HeadSHA)
		if err == nil {
			return nil
		}
		attempts = append(attempts, fmt.Errorf("merge-base after fetching %s from %s: %w", spec.Ref, spec.Remote, err))
	}
	return fmt.Errorf("unable to resolve a merge base for %s...%s after fetching base history: %w", req.Range.BaseSHA, req.Range.HeadSHA, errors.Join(attempts...))
}

func runFetches(ctx context.Context, req *runRequest, fetch gitFetcher) error {
	if len(req.Fetches) == 0 {
		return nil
	}
	if fetch == nil {
		return errors.New("history fetch is required but no fetcher was configured")
	}
	for _, spec := range req.Fetches {
		if err := validateFetchSpec(spec); err != nil {
			return err
		}
		if _, err := fetch(ctx, req.RepoRoot, spec); err != nil {
			return fmt.Errorf("fetch %s from %s: %w", spec.Ref, spec.Remote, err)
		}
	}
	return nil
}

func validateFetchSpec(spec fetchSpec) error {
	if spec.Remote == "" || spec.Ref == "" {
		return fmt.Errorf("invalid fetch spec: remote and ref are required")
	}
	return nil
}

func gitMergeBase(ctx context.Context, repoRoot string, git gitRunner, left, right string) (string, error) {
	result, err := git(ctx, repoRoot, "merge-base", left, right)
	if err != nil {
		return "", err
	}
	base := strings.TrimSpace(result.Stdout)
	if base == "" {
		return "", fmt.Errorf("git merge-base %s %s returned no revision", left, right)
	}
	return base, nil
}

func execGitFetch(ctx context.Context, dir string, spec fetchSpec) (gitResult, error) {
	return execGit(ctx, dir, "fetch", "--no-tags", spec.Remote, spec.Ref)
}

func validateRef(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("%s must not start with '-': %q", name, value)
	}
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s must not contain invalid bytes", name)
	}
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return fmt.Errorf("%s must be a safe branch ref: %q", name, value)
	}
	if strings.Contains(value, "..") || strings.Contains(value, "@{") || strings.HasSuffix(value, ".lock") {
		return fmt.Errorf("%s must be a safe branch ref: %q", name, value)
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return fmt.Errorf("%s must not contain control or whitespace characters: %q", name, value)
		}
		switch r {
		case ':', '^', '~', '?', '*', '[', '\\':
			return fmt.Errorf("%s must be a safe branch ref: %q", name, value)
		}
	}
	for segment := range strings.SplitSeq(value, "/") {
		if segment == "" || strings.HasPrefix(segment, ".") {
			return fmt.Errorf("%s must be a safe branch ref: %q", name, value)
		}
	}
	return nil
}

func validateRepoFullName(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if !repoFullNameRE.MatchString(value) || strings.Contains(value, "..") {
		return fmt.Errorf("%s must be a GitHub owner/repository name: %q", name, value)
	}
	return nil
}

func githubRepoURL(fullName string) string {
	return "https://github.com/" + fullName + ".git"
}

func branchFetchRef(ref string) string {
	return "refs/heads/" + ref
}

func remoteTrackingRefspec(ref string) string {
	return branchFetchRef(ref) + ":refs/remotes/origin/" + ref
}
