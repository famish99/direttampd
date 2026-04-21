package slimproto

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

// stageResult reports the outcome of fetching an LMS HTTP stream to disk.
type stageResult struct {
	Path       string
	BytesRead  int64
	StatusCode int
}

// stageToFile downloads a single HTTP URL (LMS stream) to a local file under
// dir and returns the local path and bytes read. It overwrites any existing
// file with the same name (Slimproto expects a fresh stream per STRM-start).
func stageToFile(url, dir, baseName string) (stageResult, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return stageResult{}, fmt.Errorf("create stage dir: %w", err)
	}
	path := filepath.Join(dir, baseName)

	log.Printf("slimproto: staging %s -> %s", url, path)

	resp, err := http.Get(url)
	if err != nil {
		return stageResult{}, fmt.Errorf("fetch stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return stageResult{StatusCode: resp.StatusCode},
			fmt.Errorf("stream HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(path)
	if err != nil {
		return stageResult{}, fmt.Errorf("create stage file: %w", err)
	}
	n, err := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if err != nil {
		os.Remove(path)
		return stageResult{}, fmt.Errorf("write stage file: %w", err)
	}
	if closeErr != nil {
		return stageResult{}, fmt.Errorf("close stage file: %w", closeErr)
	}

	return stageResult{Path: path, BytesRead: n, StatusCode: resp.StatusCode}, nil
}
