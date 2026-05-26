package gitlog

import (
"context"
"crypto/sha256"
"fmt"
"log/slog"
"strings"

"github.com/quyenluc/assiter/internal/umodel"
)

// GraphWriter is satisfied by graph.Client — kept as interface so it's testable.
type GraphWriter interface {
UpsertGraph(ctx context.Context, g *umodel.Graph) error
}

// IngestResult summarises a git-log ingestion run.
type IngestResult struct {
CommitsIngested  int
FilesLinked      int
TicketsFound     int
FileLinksCreated int // alias for CLI output
TicketsLinked    int // alias for CLI output
}

// Ingest extracts git history from repoDir and upserts it into the graph.
func Ingest(ctx context.Context, repoDir string, w GraphWriter) (*IngestResult, error) {
slog.Info("git history ingestion started", "dir", repoDir)

commits, err := Extract(repoDir)
if err != nil {
return nil, fmt.Errorf("extracting git history: %w", err)
}
slog.Info("git log extracted", "commits", len(commits))

g := umodel.NewGraph()
seenTicket := map[string]bool{}
fileCount := 0

for _, c := range commits {
commitNodeID := CommitID(c.Hash)

// ── Commit node (always created, with or without ticket IDs) ─────
g.AddNode(&umodel.Node{
ID:   commitNodeID,
Type: umodel.NodeCommit,
Name: c.Hash[:8] + " " + truncate(c.Message, 60),
Properties: map[string]string{
"hash":      c.Hash,
"date":      c.Date.Format("2006-01-02T15:04:05Z"),
"author":    c.Author,
"message":   c.Message,
"ticketIds": strings.Join(c.TicketIDs, ","),
},
})

// ── Ticket nodes + edges (only when IDs are found in the message) ─
for _, tid := range c.TicketIDs {
tNodeID := TicketID(tid)
if !seenTicket[tNodeID] {
seenTicket[tNodeID] = true
g.AddNode(&umodel.Node{
ID:   tNodeID,
Type: umodel.NodeTicket,
Name: tid,
Properties: map[string]string{
"ticketId": tid,
},
})
}
g.AddEdge(&umodel.Edge{
FromID:   commitNodeID,
ToID:     tNodeID,
Type:     umodel.EdgeMentions,
FromType: umodel.NodeCommit,
ToType:   umodel.NodeTicket,
})
}

// ── File→Commit edges with changed line ranges ────────────────────
for _, fc := range c.FileChanges {
fID := fileNodeID(fc.Path)

// Flatten line ranges into [start1, end1, start2, end2, ...]
var flatRanges []int64
for _, r := range fc.LineRanges {
flatRanges = append(flatRanges, int64(r.Start), int64(r.End))
}

g.AddEdge(&umodel.Edge{
FromID:   fID,
ToID:     commitNodeID,
Type:     umodel.EdgeHasCommit,
FromType: umodel.NodeFile,
ToType:   umodel.NodeCommit,
Properties: map[string]string{
"date":    c.Date.Format("2006-01-02"),
"author":  c.Author,
"message": truncate(c.Message, 120),
},
IntLists: map[string][]int64{
"changedRanges": flatRanges,
},
})
fileCount++
}
}

slog.Info("git graph built",
"commit_nodes", len(commits),
"ticket_nodes", len(seenTicket),
"file_edges", fileCount,
)

if err := w.UpsertGraph(ctx, g); err != nil {
return nil, fmt.Errorf("upserting git graph: %w", err)
}

slog.Info("git history ingestion complete",
"commits", len(commits),
"tickets", len(seenTicket),
)

return &IngestResult{
CommitsIngested:  len(commits),
FilesLinked:      fileCount,
TicketsFound:     len(seenTicket),
FileLinksCreated: fileCount,
TicketsLinked:    len(seenTicket),
}, nil
}

// fileNodeID reproduces the deterministic ID the normalizer assigns to File nodes.
func fileNodeID(absPath string) string {
return deterministicID("File", absPath)
}

// deterministicID mirrors normalizer.nodeID exactly.
func deterministicID(nodeType, key string) string {
h := sha256.Sum256([]byte(nodeType + ":" + key))
return strings.ToLower(nodeType) + "_" + fmt.Sprintf("%x", h[:8])
}

func truncate(s string, n int) string {
r := []rune(s)
if len(r) <= n {
return s
}
return string(r[:n]) + "…"
}
