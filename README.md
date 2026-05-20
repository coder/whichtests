# whichtests

`whichtests` is the Go test-plan generator that drives the `flake-go` CI
workflow in `coder/coder`. Given a base/head git revision pair (or a
GitHub Actions event), it walks the diff, parses each changed test
file, picks the smallest set of tests to rerun, and emits a workflow
matrix plus a human-readable Markdown summary.

## Building and running

```sh
go build ./
./whichtests --help
```

Typical invocation against the local working tree:

```sh
./whichtests \
  --repo-root . \
  --base-sha origin/main \
  --head-sha HEAD \
  --out-matrix ./flake-matrix.json \
  --out-summary -
```

In GitHub Actions:

```sh
go run ./ \
  --repo-root . \
  --github-actions \
  --out-matrix "$RUNNER_TEMP/flake-matrix.json"
```

## File layout

The binary is a single `package main`, split into focused files:

| File            | Responsibility                                                      |
| --------------- | ------------------------------------------------------------------- |
| `cli.go`        | `main`, flag parsing, command orchestration (`run`, `runCommand`).  |
| `config.go`     | `config` / `commandConfig` types and defaults.                      |
| `request.go`    | `runRequest`, `diffRange`, revision validation.                     |
| `gitexec.go`    | `gitRunner` / `gitFetcher` types and the real `exec.Command` impl.  |
| `diff.go`       | Reading and parsing `git diff`, change kinds, hunks, line ranges.   |
| `snapshot.go`   | AST snapshot parsing, `fileSnapshot`, `sharedDecl`, fallbacks.      |
| `broadening.go` | Per-kind broadening rules (`broadeningScope`).                      |
| `selection.go`  | Per-change selection logic (`selectChange`, broaden vs narrow).     |
| `inventory.go`  | `inventoryCache` for package/directory test discovery.              |
| `plan.go`       | Plan construction, matrix and summary rendering (`buildExecutionPlan`, `selectTestPlan`). |
| `githubactions.go` | GitHub Actions request builder and history preparation.          |
| `publish.go`    | Single sink for matrix and summary outputs.                         |

## Testing

```sh
go test ./...
```
