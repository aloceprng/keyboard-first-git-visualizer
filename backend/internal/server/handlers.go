package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/aloceprng/keyboard-first-git-visualizer/internal/action"
	gitpkg "github.com/aloceprng/keyboard-first-git-visualizer/internal/git"
	"github.com/aloceprng/keyboard-first-git-visualizer/internal/graph"
)

var destructiveActions = map[string]bool{
	"reset_hard": true,
	"branch_delete": true,
	"tag_delete": true,
}

// serves GET /graph — paginated topological row list
// 	 limit: rows per page (default 500)
//   before: SHA of the last seen commit; omit for the first page
//   filter: branch name to restrict to, or "all" (default)
//   response: NDJSON — one JSON object per line, flushed every 50 rows
func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	q := r.URL.Query()

	limit := 500
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 5000 {
			limit = n
		}
	}

	before := q.Get("before")
	filter := q.Get("filter")

	rows := s.graph.Rows

	if filter != "" && filter != "all" { rows = filterRowsByBranch(rows, filter, s.graph) }

	start := 0
	if before != "" {
		for i, row := range rows {
			if row.Commit.SHA == before {
				start = i + 1
				break
			}
		}
	}

	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}

	page := rows[start:end]
	hasMore := end < len(rows)

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Has-More", strconv.FormatBool(hasMore))

	flusher, canFlush := w.(http.Flusher)
	enc := json.NewEncoder(w)

	for i, row := range page {
		if err := enc.Encode(toRowResponse(row)); err != nil { return }
		if canFlush && i%50 == 0 { flusher.Flush() }
	}

	if canFlush { flusher.Flush() }
}

// dispatches between /commit/:sha (metadata) and /commit/:sha/diff (unified diff)
func (s *Server) handleCommitRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/commit/")

	if strings.HasSuffix(rest, "/diff") {
		sha := strings.TrimSuffix(rest, "/diff")
		if err := validateSHA(sha); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.handleCommitDiff(w, r, sha)
		return
	}

	sha := rest
	if err := validateSHA(sha); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.handleCommit(w, r, sha)
}

// serves full commit metadata (no diff content)
func (s *Server) handleCommit(w http.ResponseWriter, r *http.Request, sha string) {
	commit, ok := s.graph.BySHA[sha]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("commit %s not found", sha[:7]))
		return
	}

	stats, changedFiles, err := commitStats(s.repo, sha)
	if err != nil {
		stats = diffStats{}
		changedFiles = nil
	}

	resp := commitDetailResponse{
		SHA: commit.SHA,
		Short: commit.ShortSHA,
		Subject: commit.Subject,
		Body: commit.Body,
		Author: commit.Author,
		Committer: commit.Committer,
		Timestamp: commit.Timestamp.Format(time.RFC3339),
		Parents: commit.ParentSHAs,
		Refs: commit.Refs,
		Stats: stats,
		ChangedFiles: changedFiles,
	}

	writeJSON(w, http.StatusOK, resp)
}

// serves GET /commit/:sha/diff — full unified diff, streamed
// each DiffFile is flushed as soon as it is available so large diffs render
// progressively rather than arriving as one big payload
func (s *Server) handleCommitDiff(w http.ResponseWriter, r *http.Request, sha string) {
	commit, err := s.repo.CommitObject(plumbing.NewHash(sha))
	if err != nil {
		writeError(w, http.StatusNotFound, "commit not found")
		return
	}

	// Diff against first parent; initial commits diff against an empty tree.
	var patch interface {
		FilePatches() []interface{} // placeholder — use actual go-git types
	}
	_ = commit
	_ = patch

	// Real implementation:
	//   parent, err := commit.Parent(0)  // first parent
	//   patch, err := parent.Patch(commit)
	//   for _, filePatch := range patch.FilePatches() { ... stream file ... }
	//
	// For initial commits (no parent):
	//   emptyTree, _ := s.repo.TreeObject(plumbing.NewHash("4b825dc642..."))
	//   patch, _ = emptyTree.Patch(commitTree)

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// actual streaming implementation goes here
}


// serves GET /refs — all refs and in-progress operation state
func (s *Server) handleRefs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	refs := make([]refResponse, 0, len(s.graph.RefIndex))
	for name, sha := range s.graph.RefIndex {
		refs = append(refs, refResponse{
			Name:      name,
			SHA:       sha,
			Type:      refType(name),
			IsCurrent: s.graph.CommitRefs[sha] != nil && isCurrentBranch(name, s.graph),
		})
	}

	inProgress, err := gitpkg.InProgressState(s.repoPath)
	if err != nil {
		inProgress = ""
	}

	meta, err := gitpkg.InProgressMeta(s.repoPath)
	if err != nil {
		meta = nil
	}

	head, headBranch := resolveHEAD(s.graph)

	writeJSON(w, http.StatusOK, refsResponse{
		Refs:               refs,
		HEAD:               head,
		HeadBranch:         headBranch,
		InProgress:         inProgress,
		InProgressMeta:     meta,
	})
}

// serves GET /search — fuzzy search over the commit corpus
//   q: search query (required)
//   type: "commit" | "author" | "branch" | "file" (default: "commit")
//   limit: max results (default 20)
// Returns 503 while the search index is still building (phase 2 not complete)
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.search == nil {
		writeError(w, http.StatusServiceUnavailable, "search index is still building")
		return
	}

	q := r.URL.Query()

	query := strings.TrimSpace(q.Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}

	kind := q.Get("type")
	if kind == "" {
		kind = "commit"
	}

	limit := 20
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	results := s.search.Search(query, kind, limit)
	writeJSON(w, http.StatusOK, results)
}


// serves POST /action — executes a git operation and streams ActionEvents as NDJSON until the operation completes
func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req action.ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Action == "" {
		writeError(w, http.StatusBadRequest, "action is required")
		return
	}

	if destructiveActions[req.Action] && !req.Confirm {
		writeError(w, http.StatusConflict, fmt.Sprintf(
			"action %q requires confirm:true in the request body", req.Action,
		))
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	flusher, canFlush := w.(http.Flusher)
	enc := json.NewEncoder(w)

	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}

	out := make(chan action.ActionEvent, 64)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.runner.Execute(r.Context(), req, out)
		close(out)
	}()

	for event := range out {
		if err := enc.Encode(event); err != nil {
			return
		}
		flush()
	}

	if err := <-errCh; err != nil {
		_ = enc.Encode(action.ActionEvent{
			Type: action.EventError,
			Data: err.Error(),
		})
		flush()
	}
}

type rowResponse struct {
	SHA         string         `json:"sha"`
	Short       string         `json:"short"`
	Subject     string         `json:"subject"`
	Author      graph.Identity `json:"author"`
	Timestamp   string         `json:"timestamp"`
	Parents     []string       `json:"parents"`
	Lane        int            `json:"lane"`
	Row         int            `json:"row"`
	Edges       []graph.Edge   `json:"edges"`
	Passthrough uint64         `json:"passthrough"`
	Refs        []string       `json:"refs"`
	ActiveLanes int            `json:"activeLanes"`
}

type diffStats struct {
	Insertions int `json:"insertions"`
	Deletions  int `json:"deletions"`
	Files      int `json:"files"`
}

type commitDetailResponse struct {
	SHA          string         `json:"sha"`
	Short        string         `json:"short"`
	Subject      string         `json:"subject"`
	Body         string         `json:"body"`
	Author       graph.Identity `json:"author"`
	Committer    graph.Identity `json:"committer"`
	Timestamp    string         `json:"timestamp"`
	Parents      []string       `json:"parents"`
	Refs         []string       `json:"refs"`
	Stats        diffStats      `json:"stats"`
	ChangedFiles []string       `json:"changedFiles"`
}

type refResponse struct {
	Name      string `json:"name"`
	SHA       string `json:"sha"`
	Type      string `json:"type"`
	IsCurrent bool   `json:"isCurrent"`
}

type refsResponse struct {
	Refs           []refResponse     `json:"refs"`
	HEAD           string            `json:"head"`
	HeadBranch     string            `json:"headBranch"`
	InProgress     string            `json:"inProgress"`
	InProgressMeta map[string]string `json:"inProgressMeta"`
}

// helpers vv

// convert a graph.Row into the flat wire format
func toRowResponse(row *graph.Row) rowResponse {
	c := row.Commit
	return rowResponse{
		SHA:         c.SHA,
		Short:       c.ShortSHA,
		Subject:     c.Subject,
		Author:      c.Author,
		Timestamp:   c.Timestamp.Format(time.RFC3339),
		Parents:     c.ParentSHAs,
		Lane:        c.Lane,
		Row:         c.Row,
		Edges:       c.Edges,
		Passthrough: c.Passthrough,
		Refs:        c.Refs,
		ActiveLanes: row.ActiveLanes,
	}
}

// returns only the rows reachable from the named branch
func filterRowsByBranch(rows []*graph.Row, branch string, g *graph.Graph) []*graph.Row {
	tipSHA, ok := g.RefIndex["refs/heads/"+branch]
	if !ok {
		tipSHA, ok = g.RefIndex[branch]
		if !ok {
			return rows
		}
	}

	reachable := make(map[string]bool)
	queue := []string{tipSHA}
	for len(queue) > 0 {
		sha := queue[0]
		queue = queue[1:]
		if reachable[sha] {
			continue
		}
		reachable[sha] = true
		if c, ok := g.BySHA[sha]; ok {
			queue = append(queue, c.ParentSHAs...)
		}
	}

	filtered := make([]*graph.Row, 0, len(rows))
	for _, row := range rows {
		if reachable[row.Commit.SHA] {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// opens the commit and its parent via go-git to read file change statistics
func commitStats(repo *gogit.Repository, sha string) (diffStats, []string, error) {
	commit, err := repo.CommitObject(plumbing.NewHash(sha))
	if err != nil { return diffStats{}, nil, err }

	stats, err := commit.Stats()
	if err != nil { return diffStats{}, nil, err }

	var insertions, deletions int
	files := make([]string, 0, len(stats))
	for _, s := range stats {
		insertions += s.Addition
		deletions += s.Deletion
		files = append(files, s.Name)
	}

	return diffStats{
		Insertions: insertions,
		Deletions:  deletions,
		Files:      len(stats),
	}, files, nil
}

// classifies a ref name into the four ref categories
func refType(name string) string {
	switch {
	case strings.HasPrefix(name, "refs/heads/"):
		return "branch"
	case strings.HasPrefix(name, "refs/remotes/"):
		return "remote"
	case strings.HasPrefix(name, "refs/tags/"):
		return "tag"
	case strings.HasPrefix(name, "refs/stash"):
		return "stash"
	default:
		return "branch"
	}
}

// returns true if name is the branch HEAD currently points to
func isCurrentBranch(name string, g *graph.Graph) bool {
	headSHA, ok := g.RefIndex["HEAD"]
	if !ok {
		return false
	}
	sha, ok := g.RefIndex[name]
	if !ok {
		return false
	}
	return sha == headSHA
}

// returns the current HEAD SHA and the branch name it points to
func resolveHEAD(g *graph.Graph) (sha string, branch string) {
	sha = g.RefIndex["HEAD"]

	for name, refSHA := range g.RefIndex {
		if strings.HasPrefix(name, "refs/heads/") && refSHA == sha {
			branch = strings.TrimPrefix(name, "refs/heads/")
			return
		}
	}
	return sha, "" // detached HEAD
}

// returns an error if sha is not a plausible git SHA
func validateSHA(sha string) error {
	if len(sha) < 7 || len(sha) > 40 {
		return fmt.Errorf("invalid SHA %q: must be 7–40 hex characters", sha)
	}
	for _, c := range sha {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return fmt.Errorf("invalid SHA %q: non-hex character %q", sha, c)
		}
	}
	return nil
}

// encodes v as JSON and writes it with the given status code
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writes a JSON error payload: {"error": "message"}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}