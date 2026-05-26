// Package gitlog extracts commit history from a git repository and models it
// as graph nodes/edges so ticket IDs, files, and functions are linked.
package gitlog

import (
"bufio"
"bytes"
"crypto/sha256"
"fmt"
"os/exec"
"path/filepath"
"regexp"
"strconv"
"strings"
"time"
)

// LineRange is an inclusive [start, end] line range (1-based) of changed lines.
type LineRange struct {
Start int
End   int
}

// FileChange holds a file path and the line ranges actually modified in a commit.
type FileChange struct {
Path       string
LineRanges []LineRange
}

// CommitRecord holds everything extracted from a single git commit.
type CommitRecord struct {
Hash        string
Date        time.Time
Author      string
Message     string
TicketIDs   []string
FileChanges []FileChange // each file with its changed line ranges
}

// ticketRE matches common ticket formats:
//   - Numeric:    3581, 12345
//   - JIRA-style: ABC-123, PROJ-4567
var ticketRE = regexp.MustCompile(`\b([A-Z]{2,}-\d+|\d{3,6})\b`)

// hunkRE parses the "+new_start[,new_count]" part of a unified diff @@ header.
// Format: @@ -old_start[,old_count] +new_start[,new_count] @@
var hunkRE = regexp.MustCompile(`\+(\d+)(?:,(\d+))?`)

// Extract runs `git log` in repoDir and returns all commits with changed line ranges.
func Extract(repoDir string) ([]*CommitRecord, error) {
// --unified=0 gives us diff hunks with no context lines, so we know exactly
// which lines changed. The COMMIT||| prefix disambiguates header lines from
// diff content lines.
cmd := exec.Command("git", "-C", repoDir, "log",
"--all",
"--unified=0",          // diff hunks with no context
"--diff-filter=ACDMRT", // skip untracked/deleted-only
"--pretty=format:COMMIT|||%H|||%aI|||%an|||%s",
)
out, err := cmd.Output()
if err != nil {
return nil, fmt.Errorf("git log: %w", err)
}

var commits []*CommitRecord
var current *CommitRecord
var currentFile *FileChange

flush := func() {
if current == nil {
return
}
if currentFile != nil {
current.FileChanges = append(current.FileChanges, *currentFile)
currentFile = nil
}
if len(current.FileChanges) > 0 {
commits = append(commits, current)
}
}

scanner := bufio.NewScanner(bytes.NewReader(out))
// increase buffer for very long diffs
scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)

for scanner.Scan() {
line := scanner.Text()

// ── New commit header ──────────────────────────────────────────────
if strings.HasPrefix(line, "COMMIT|||") {
flush()
parts := strings.SplitN(line, "|||", 5)
if len(parts) < 5 {
current = nil
continue
}
t, _ := time.Parse(time.RFC3339, parts[2])
msg := parts[4]
current = &CommitRecord{
Hash:      parts[1],
Date:      t,
Author:    parts[3],
Message:   msg,
TicketIDs: extractTickets(msg),
}
currentFile = nil
continue
}

if current == nil {
continue
}

// ── New file in diff ───────────────────────────────────────────────
// "diff --git a/path b/path" — use b/ path as the file
if strings.HasPrefix(line, "diff --git ") {
if currentFile != nil {
current.FileChanges = append(current.FileChanges, *currentFile)
}
currentFile = nil
// parse the b/ path
parts := strings.Fields(line)
if len(parts) >= 4 {
bPath := strings.TrimPrefix(parts[3], "b/")
abs := filepath.Join(repoDir, bPath)
currentFile = &FileChange{Path: abs}
}
continue
}

// "+++ b/path" gives us the canonical new-file path (handles renames)
if strings.HasPrefix(line, "+++ b/") && currentFile != nil {
bPath := strings.TrimPrefix(line, "+++ b/")
currentFile.Path = filepath.Join(repoDir, bPath)
continue
}

// ── Hunk header ────────────────────────────────────────────────────
// "@@ -old +new[,count] @@" — extract changed line range in new file
if strings.HasPrefix(line, "@@") && currentFile != nil {
m := hunkRE.FindStringSubmatch(line)
if m != nil {
start, _ := strconv.Atoi(m[1])
count := 1
if m[2] != "" {
count, _ = strconv.Atoi(m[2])
}
if count == 0 {
// Pure deletion — no lines added in new file, skip
continue
}
end := start + count - 1
currentFile.LineRanges = append(currentFile.LineRanges, LineRange{Start: start, End: end})
}
continue
}
}
flush()

return commits, scanner.Err()
}

// extractTickets finds all ticket IDs in a commit message.
func extractTickets(msg string) []string {
seen := map[string]bool{}
var out []string
for _, m := range ticketRE.FindAllString(strings.ToUpper(msg), -1) {
if !seen[m] {
seen[m] = true
out = append(out, m)
}
}
return out
}

// CommitID returns a deterministic graph node ID for a commit.
func CommitID(hash string) string {
h := sha256.Sum256([]byte("commit:" + hash))
return fmt.Sprintf("%x", h[:8])
}

// TicketID returns a deterministic graph node ID for a ticket.
func TicketID(ticketKey string) string {
h := sha256.Sum256([]byte("ticket:" + strings.ToUpper(ticketKey)))
return fmt.Sprintf("%x", h[:8])
}
