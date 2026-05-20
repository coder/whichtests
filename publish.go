package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const defaultGitHubOutputValueLimit = 1024 * 1024

func publishPlan(sinks outputSinks, matrix matrixOutput, summary string, stdout io.Writer) error {
	matrixData, err := marshalMatrix(matrix)
	if err != nil {
		return err
	}
	if sinks.OutMatrix != "" {
		if err := writeFile(sinks.OutMatrix, appendNewline(matrixData)); err != nil {
			return err
		}
	}
	if sinks.OutSummary != "" {
		if err := writeSummary(sinks.OutSummary, summary, stdout); err != nil {
			return err
		}
	}
	if sinks.GitHubOutput != "" {
		if err := appendGitHubOutput(sinks.GitHubOutput, "matrix", string(matrixData), 0); err != nil {
			return err
		}
	}
	if sinks.GitHubStepSummary != "" {
		if err := appendFile(sinks.GitHubStepSummary, []byte(summary)); err != nil {
			return err
		}
	}
	return nil
}

func marshalMatrix(matrix matrixOutput) ([]byte, error) {
	if matrix.Include == nil {
		matrix.Include = []matrixEntry{}
	}
	data, err := json.Marshal(matrix)
	if err != nil {
		return nil, fmt.Errorf("marshal matrix json: %w", err)
	}
	return data, nil
}

func appendGitHubOutput(path, name, value string, outputSizeLimit int) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("GitHub output %s must be a single line", name)
	}
	if outputSizeLimit == 0 {
		outputSizeLimit = defaultGitHubOutputValueLimit
	}
	if len(value) > outputSizeLimit {
		return fmt.Errorf("GitHub output %s is %d bytes, above the %d byte limit", name, len(value), outputSizeLimit)
	}
	return appendFile(path, []byte(name+"="+value+"\n"))
}

func writeSummary(path, summary string, stdout io.Writer) error {
	if path == "-" {
		_, err := io.WriteString(stdout, summary)
		return err
	}
	return writeFile(path, []byte(summary))
}

func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func appendFile(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if dir != "." {
		if err = os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	// #nosec G304: path is a user-supplied output path or a GitHub Actions runner path.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() {
		// Surface Close errors only if Write succeeded; write paths can
		// lose data on a deferred fsync/flush failure.
		if cerr := file.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", path, cerr)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("append %s: %w", path, err)
	}
	return nil
}

func appendNewline(data []byte) []byte {
	withNewline := make([]byte, 0, len(data)+1)
	withNewline = append(withNewline, data...)
	withNewline = append(withNewline, '\n')
	return withNewline
}
