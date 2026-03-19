// Package checkpoint provides the ResultStore — append-only NDJSON storage for scan results.
package checkpoint

import (
"bufio"
"encoding/json"
"fmt"
"log/slog"
"os"
"sync"

"github.com/mona-actions/gh-repo-map/internal/model"
)

// RepoResult holds the scan results for a single repository.
type RepoResult struct {
FullName     string           `json:"full_name"`
SBOMData     *model.SBOMData  `json:"sbom_data,omitempty"`
FileScanData *model.FileScanData `json:"filescan_data,omitempty"`
}

// ResultStore provides append-only NDJSON storage for scan results.
// Each line is a JSON-encoded RepoResult. On crash, the last partial
// line is discarded (one repo re-scanned on resume).
type ResultStore struct {
mu   sync.Mutex
path string
file *os.File
enc  *json.Encoder
}

// NewResultStore opens (or creates) an NDJSON results file for appending.
func NewResultStore(path string) (*ResultStore, error) {
f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
if err != nil {
return nil, fmt.Errorf("opening results file: %w", err)
}
rs := &ResultStore{
path: path,
file: f,
enc:  json.NewEncoder(f),
}
return rs, nil
}

// Append writes a single repo result as one JSON line.
func (rs *ResultStore) Append(result RepoResult) error {
rs.mu.Lock()
defer rs.mu.Unlock()
return rs.enc.Encode(result)
}

// LoadAll reads all results from the NDJSON file into a map keyed by repo full name.
// Duplicate entries use last-entry-wins semantics (handles retries).
// A truncated last line (crash mid-write) is silently skipped with a warning log.
func LoadAll(path string) (map[string]*RepoResult, error) {
f, err := os.Open(path)
if err != nil {
if os.IsNotExist(err) {
return make(map[string]*RepoResult), nil
}
return nil, fmt.Errorf("opening results file: %w", err)
}
defer f.Close()

results := make(map[string]*RepoResult)
scanner := bufio.NewScanner(f)
// Allow up to 10MB per line for large repos
scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
lineNum := 0

for scanner.Scan() {
lineNum++
line := scanner.Bytes()
if len(line) == 0 {
continue
}
var r RepoResult
if err := json.Unmarshal(line, &r); err != nil {
slog.Warn("skipping corrupt result line", "line", lineNum, "error", err)
continue
}
results[r.FullName] = &r
}

if err := scanner.Err(); err != nil {
return nil, fmt.Errorf("reading results file: %w", err)
}

return results, nil
}

// Close flushes and closes the results file.
func (rs *ResultStore) Close() error {
rs.mu.Lock()
defer rs.mu.Unlock()
if rs.file != nil {
return rs.file.Close()
}
return nil
}

// Path returns the file path of the result store.
func (rs *ResultStore) Path() string {
return rs.path
}

// Delete removes the results file from disk.
func DeleteResults(path string) error {
return os.Remove(path)
}
