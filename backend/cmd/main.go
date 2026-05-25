package cmd

import (
	"context"
	"crypto/md5"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

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

	sockPath := socketPath(repoRoot)
	if checkExistingProcess(sockPath) {
		fmt.Printf("server already running for %s at %s\n", repoRoot, sockPath)
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
	g, err := loadGraph(ctx, repoRoot, repo, func(addedSHAs []string) {
		if srv != nil {
			srv.BroadcastUpdate(server.WatchEvent{
				Type: "graph_updated",
				AddedSHAs: addedSHAs,
			})
		}
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading graph: %v\n", err)
		os.Exit(1)
	}

	srv = server.New(repoRoot, repo, g)

	commits := make([]*graph.Commit, 0, len(g.Rows))
	for _, row := range g.Rows {
		if row.Commit != nil {
			commits = append(commits, row.Commit)
		}
	}
	searchIdx := search.Build(commits)
	srv.WireSearch(searchIdx)

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
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating file watcher: %v\n", err)
		os.Exit(1)
	}
	
	srv.WireWatcher(w)
	if err := w.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "error starting watcher: %v\n", err)
		os.Exit(1)
	}

	listener, err := bindSocket(sockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error binding socket: %v\n", err)
		os.Exit(1)
	}

	defer listener.Close()

	srv.StartIdleTimer(cancel)

	fmt.Fprintln(os.Stdout, "ready")
	if err := srv.StartOnListener(ctx, listener); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

// derives the Unix socket path for this repo from its absolute path
func socketPath(repoPath string) string {
	hash := md5.Sum([]byte(repoPath))
	hashStr := fmt.Sprintf("%x", hash)[:12]

	cacheDir := filepath.Join(os.TempDir(), "keyboard-first-git-visualizer")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		cacheDir = os.TempDir()
	}

	return filepath.Join(cacheDir, hashStr+".sock")
}

// probes the socket to see if a live server is already listening
// returns true if the frontend should reattach instead of spawning
func checkExistingProcess(sockPath string) bool {
	conn, err := net.DialTimeout("unix", sockPath, 500*time.Millisecond)
	if err != nil { return false }
	
	defer conn.Close()

	return true
}

// creates the Unix socket and returns the net.Listener
func bindSocket(sockPath string) (net.Listener, error) {
	_ = os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil { return nil, fmt.Errorf("failed to bind socket %s: %w", sockPath, err) }

	if err := os.Chmod(sockPath, 0600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("failed to chmod socket: %w", err)
	}

	return listener, nil
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