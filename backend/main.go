package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"net/http"

	"github.com/aloceprng/keyboard-first-git-visualizer/backend/internal/git"
	"github.com/aloceprng/keyboard-first-git-visualizer/backend/internal/graph"
	"github.com/aloceprng/keyboard-first-git-visualizer/backend/internal/search"
	"github.com/aloceprng/keyboard-first-git-visualizer/backend/internal/server"
	"github.com/aloceprng/keyboard-first-git-visualizer/backend/internal/watcher"
	gogit "github.com/go-git/go-git/v5"
)

func main() {
	repoPath := "."
	if len(os.Args) > 1 { repoPath = os.Args[1] }

	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "can't resolve path: %v\n", err)
		os.Exit(1)
	}

	repoRoot, err := git.FindRepoRoot(absPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "can't finding git repository: %v\n", err)
		os.Exit(1)
	}

	if isAlreadyRunning() {
		fmt.Println("ready")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	repo, err := git.OpenRepo(repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening repository: %v\n", err)
		os.Exit(1)
	}

	var srv *server.Server
	g, err := loadGraph(ctx, repoRoot, repo, func(added []string) { phase2Broadcast(srv, added) })
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading graph: %v\n", err)
		os.Exit(1)
	}

	srv = server.New(repoRoot, repo, g)
	srv.WireSearch(search.Build(commitsFromGraph(g)))

	w, err := newWatcher(srv, repoRoot, repo, g)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error starting file watcher: %v\n", err)
		os.Exit(1)
	}
	srv.WireWatcher(w)

	// let the frontend switch repositories at runtime via POST /open
	srv.SetOpener(func(path string) error {
		return loadRepoInto(ctx, srv, path)
	})

	srv.StartIdleTimer(cancel)

	fmt.Fprintln(os.Stdout, "ready")

	if err := srv.Start(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

// phase 1: first 500 commits, makes the server ready 
// phase 2: full history, runs in background goroutine
func loadGraph(ctx context.Context, repoPath string, repo *gogit.Repository, onPhase2 func(addedSHAs []string)) (*graph.Graph, error) {
	tips, err := git.WalkAllRefs(repo)
	if err != nil {	return nil, fmt.Errorf("failed to walk refs: %w", err) }

	refToSHA, shaToRef, err := git.BuildRefIndex(repo)
	if err != nil {	return nil, fmt.Errorf("failed to build ref index: %w", err) }

	commits, err := git.CollectCommits(repo, tips, shaToRef)
	if err != nil { return nil, fmt.Errorf("failed to collect commits: %w", err) }

	sorted, err := git.TopologicalSort(commits)
	if err != nil {	return nil, fmt.Errorf("failed to sort commits: %w", err) }

	// phase 1
	phase1Commits := sorted
	if len(sorted) > 500 { phase1Commits = sorted[:500] }

	rows, err := git.AssignLanes(phase1Commits)
	if err != nil { return nil, fmt.Errorf("failed to assign lanes: %w", err) }

	g := &graph.Graph{
		Rows: rows,
		BySHA: buildBySHAMap(phase1Commits),
		RefIndex: refToSHA,
		CommitRefs: buildCommitRefsMap(phase1Commits),
		TotalLanes: calculateTotalLanes(rows),
	}

	// phase 2
	if len(sorted) > 500 {
		go func() {
			allRows, err := git.AssignLanes(sorted)
			if err != nil { return }

			addedSHAs := make([]string, 0, len(sorted)-len(phase1Commits))
			for _, c := range sorted[len(phase1Commits):] {
				addedSHAs = append(addedSHAs, c.SHA)
			}

			g.Lock()
			g.Rows = allRows
			g.BySHA = buildBySHAMap(sorted)
			g.CommitRefs = buildCommitRefsMap(sorted)
			g.TotalLanes = calculateTotalLanes(allRows)
			g.Unlock()

			onPhase2(addedSHAs)
		}()
	}

	return g, nil
}

// opens the repo at path, rebuilds the graph/search/watcher, and swaps it all
// into the running server. Backs the POST /open endpoint.
func loadRepoInto(ctx context.Context, srv *server.Server, path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("can't resolve path: %w", err)
	}

	repoRoot, err := git.FindRepoRoot(absPath)
	if err != nil {
		return fmt.Errorf("not a git repository: %w", err)
	}

	repo, err := git.OpenRepo(repoRoot)
	if err != nil {
		return fmt.Errorf("error opening repository: %w", err)
	}

	g, err := loadGraph(ctx, repoRoot, repo, func(added []string) { phase2Broadcast(srv, added) })
	if err != nil {
		return fmt.Errorf("error loading graph: %w", err)
	}

	w, err := newWatcher(srv, repoRoot, repo, g)
	if err != nil {
		return fmt.Errorf("error starting file watcher: %w", err)
	}

	srv.Reset(repoRoot, repo, g, search.Build(commitsFromGraph(g)), w)
	return nil
}

// creates and starts a file watcher that keeps the graph's ref index live.
func newWatcher(srv *server.Server, repoRoot string, repo *gogit.Repository, g *graph.Graph) (*watcher.Watcher, error) {
	w, err := watcher.New(repoRoot, func(event watcher.InvalidationEvent) {
		if !event.RefsChanged { return }

		refToSHA, _, err := git.BuildRefIndex(repo)
		if err != nil { return }

		g.Lock()
		g.RefIndex = refToSHA
		g.Unlock()

		srv.BroadcastUpdate(server.WatchEvent{
			Type: "refs_changed",
			InProgress: event.InProgress,
		})
	})
	if err != nil { return nil, err }

	if err := w.Start(); err != nil { return nil, err }
	return w, nil
}

// flattens a graph's rows into the commit slice the search index expects.
func commitsFromGraph(g *graph.Graph) []*graph.Commit {
	commits := make([]*graph.Commit, 0, len(g.Rows))
	for _, row := range g.Rows {
		if row.Commit != nil {
			commits = append(commits, row.Commit)
		}
	}
	return commits
}

// notifies WebSocket clients of phase-2 commits, once the server exists.
func phase2Broadcast(srv *server.Server, addedSHAs []string) {
	if srv == nil { return }
	srv.BroadcastUpdate(server.WatchEvent{
		Type: "graph_updated",
		AddedSHAs: addedSHAs,
	})
}

// helper functions
func buildBySHAMap(commits []*graph.Commit) map[string]*graph.Commit {
	m := make(map[string]*graph.Commit)
	for _, c := range commits {
		m[c.SHA] = c
	}
	return m
}

func buildCommitRefsMap(commits []*graph.Commit) map[string][]string {
	m := make(map[string][]string)
	for _, c := range commits {
		if len(c.Refs) > 0 {
			m[c.SHA] = c.Refs
		}
	}
	return m
}

func calculateTotalLanes(rows []*graph.Row) int {
	maxLane := 0
	for _, row := range rows {
		if row.Commit != nil && row.Commit.Lane > maxLane {
			maxLane = row.Commit.Lane
		}
	}
	return maxLane + 1
}

func isAlreadyRunning() bool {
    client := &http.Client{Timeout: 300 * time.Millisecond}
    resp, err := client.Get("http://127.0.0.1:7832/ping")
    if err != nil { return false }
    resp.Body.Close()
    return resp.StatusCode == http.StatusOK
}