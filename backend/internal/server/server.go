package server

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/aloceprng/keyboard-first-git-visualizer/internal/action"
	"github.com/aloceprng/keyboard-first-git-visualizer/internal/graph"
	"github.com/aloceprng/keyboard-first-git-visualizer/internal/search"
	"github.com/aloceprng/keyboard-first-git-visualizer/internal/watcher"
	gogit "github.com/go-git/go-git/v5"
)

const (
	idleTimeout = 5 * time.Minute
	listenAddr = "127.0.0.1:7832"
)

type Server struct {
	repoPath string
	repo     *gogit.Repository
	graph    *graph.Graph
	search   *search.Index
	runner   *action.Runner
	watcher  *watcher.Watcher
	hub      *wsHub

	mux        *http.ServeMux
	httpServer *http.Server

	idleMu    sync.Mutex
	idleTimer *time.Timer
}

// wires the server with the minimum needed for phase-1 serving
func New(repoPath string, repo *gogit.Repository, g *graph.Graph) *Server {
	s := &Server{
		repoPath: repoPath,
		repo: repo,
		graph: g,
		hub: newHub(),
		mux: http.NewServeMux(),
		runner: action.New(repoPath),
	}
	s.setupRoutes()

	return s
}

// plugs in the search index once it is ready
func (s *Server) WireSearch(idx *search.Index) { s.search = idx }

// plugs in the file watcher once it is started
func (s *Server) WireWatcher(w *watcher.Watcher) { s.watcher = w }

// begins serving HTTP on listenAddr and blocks until the context is
// cancelled or the server is shut down. also starts watcherhub
func (s *Server) Start(ctx context.Context) error {
	go s.hub.run()

	s.httpServer = &http.Server{
		Addr:    listenAddr,
		Handler: s.withMiddleware(s.mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutCtx)
	}()

	err := s.httpServer.ListenAndServe()
	if err == http.ErrServerClosed { return nil }

	return err
}

// binds to an already-opened net.Listener, used by main.go when the Unix socket path is passed in directly
func (s *Server) StartOnListener(ctx context.Context, l net.Listener) error {
	go s.hub.run()

	s.httpServer = &http.Server{
		Handler: s.withMiddleware(s.mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second,
		IdleTimeout: 120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutCtx)
	}()

	err := s.httpServer.Serve(l)
	if err == http.ErrServerClosed { return nil }

	return err
}

// register all URL patterns. Every route is registered
// immediately so curl probing works from phase 1 onwards, even before
// search/actions are wired. Handlers return 503 when their dependency is nil.
func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/graph", s.handleGraph)
	s.mux.HandleFunc("/commit/", s.handleCommitRoute)
	s.mux.HandleFunc("/refs", s.handleRefs)
	s.mux.HandleFunc("/search", s.handleSearch)
	s.mux.HandleFunc("/action", s.handleAction)
	s.mux.HandleFunc("/watch", s.handleWatch)

	s.mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	})
}

// wrap every request with CORS headers and idle-timer reset
func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		s.resetIdleTimer()
		next.ServeHTTP(w, r)
	})
}

// starts the 5-minute lazy-daemon idle timer
// returns a reset function that main.go middleware calls on every request
func (s *Server) StartIdleTimer(cancel context.CancelFunc) {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	s.idleTimer = time.AfterFunc(idleTimeout, cancel)
}

// extends the idle deadline by another 5 minutes
// called by withMiddleware on every incoming request
func (s *Server) resetIdleTimer() {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	if s.idleTimer != nil {
		s.idleTimer.Reset(idleTimeout)
	}
}

// push a graph invalidation event to all connected WebSocket clients
func (s *Server) BroadcastUpdate(event WatchEvent) { s.hub.broadcast <- event }