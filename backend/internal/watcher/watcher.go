package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"github.com/fsnotify/fsnotify"
)

const debounceInterval = 80 * time.Millisecond

// describes what changed in .git after one debounce window
type InvalidationEvent struct {
	RefsChanged bool
	IndexChanged bool
	InProgress string
}

type Watcher struct {
	repoPath string
	fsw *fsnotify.Watcher
	onChange func(InvalidationEvent)

	stopCh chan struct{}
	once sync.Once 
}

// create a Watcher
func New(repoPath string, onChange func(InvalidationEvent)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil { return nil, err }

	return &Watcher{
		repoPath: repoPath,
		fsw: fsw,
		onChange: onChange,
		stopCh: make(chan struct{}),
	}, nil
}

// adds the target paths to fsnotify and launches the debounce loop
func (w *Watcher) Start() error {
	for _, path := range watchedPaths(w.repoPath) {
		if _, err := os.Stat(path); err == nil {
			if err := w.fsw.Add(path); err != nil {
				_ = err
			}
		}
	}

	gitDir := filepath.Join(w.repoPath, ".git")
	if err := w.fsw.Add(gitDir); err != nil { return err }

	go w.loop()
	return nil
}

// shuts down the fsnotify watcher and the debounce loop
func (w *Watcher) Stop() error {
	var err error
	w.once.Do(func() {
		close(w.stopCh)
		err = w.fsw.Close()
	})

	return err
}

// debounce engine - collects raw fsnotify events into an 80ms
// window, merges them, and calls onChange exactly once per window
func (w *Watcher) loop() {
	var (
		pending []InvalidationEvent
		timer   *time.Timer
		timerCh <-chan time.Time
	)

	fire := func() {
		if len(pending) == 0 { return }
		merged := mergeEvents(pending)
		pending = pending[:0]
		timerCh = nil

		w.refreshInProgressPaths()

		w.onChange(merged)
	}

	for {
		select {

		case event, ok := <-w.fsw.Events:
			if !ok { return }

			op := event.Op
			if op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 { continue }

			inv := classifyChange(event.Name, w.repoPath)

			if inv == (InvalidationEvent{}) { continue }

			pending = append(pending, inv)

			if timer != nil { timer.Stop() }
			timer = time.AfterFunc(debounceInterval, func() {})
			timerCh = timer.C

		case <-timerCh:
			fire()

		case _, ok := <-w.fsw.Errors:
			if !ok { return }

		case <-w.stopCh:
			if timer != nil { timer.Stop() }
			return
		}
	}
}

// returns the specific .git file and directory paths that warrant watching
func watchedPaths(repoPath string) []string {
	gitDir := filepath.Join(repoPath, ".git")

	return []string{
		filepath.Join(gitDir, "HEAD"),
		filepath.Join(gitDir, "refs", "heads"),
		filepath.Join(gitDir, "refs", "remotes"),

		filepath.Join(gitDir, "packed-refs"),

		filepath.Join(gitDir, "index"),

		filepath.Join(gitDir, "MERGE_HEAD"),
		filepath.Join(gitDir, "CHERRY_PICK_HEAD"),
		filepath.Join(gitDir, "REVERT_HEAD"),
		filepath.Join(gitDir, "rebase-merge"),
		filepath.Join(gitDir, "rebase-apply"),
	}
}

// maps a raw .git file path to the InvalidationEvent fields it should set
func classifyChange(path, repoPath string) InvalidationEvent {
	gitDir := filepath.Join(repoPath, ".git")
	
	rel, err := filepath.Rel(gitDir, path)
	if err != nil {
		return InvalidationEvent{}
	}

	rel = filepath.ToSlash(rel)

	switch {
	case rel == "HEAD":
		return InvalidationEvent{RefsChanged: true}

	case rel == "packed-refs":
		return InvalidationEvent{RefsChanged: true}

	case strings.HasPrefix(rel, "refs/heads/"):
		return InvalidationEvent{RefsChanged: true}

	case strings.HasPrefix(rel, "refs/remotes/"):
		return InvalidationEvent{RefsChanged: true}

	case strings.HasPrefix(rel, "refs/tags/"):
		return InvalidationEvent{RefsChanged: true}

	case rel == "index":
		return InvalidationEvent{IndexChanged: true}

	case rel == "MERGE_HEAD":
		return InvalidationEvent{RefsChanged: true, InProgress: "merge"}

	case rel == "CHERRY_PICK_HEAD":
		return InvalidationEvent{RefsChanged: true, InProgress: "cherry-pick"}

	case rel == "REVERT_HEAD":
		return InvalidationEvent{RefsChanged: true, InProgress: "revert"}

	case rel == "rebase-merge" || strings.HasPrefix(rel, "rebase-merge/"):
		return InvalidationEvent{RefsChanged: true, InProgress: "rebase"}

	case rel == "rebase-apply" || strings.HasPrefix(rel, "rebase-apply/"):
		return InvalidationEvent{RefsChanged: true, InProgress: "rebase"}

	default:
		return InvalidationEvent{}
	}
}

// folds all events from one debounce window into a single InvalidationEvent
func mergeEvents(events []InvalidationEvent) InvalidationEvent {
	var merged InvalidationEvent
	for _, e := range events {
		if e.RefsChanged {
			merged.RefsChanged = true
		}
		if e.IndexChanged {
			merged.IndexChanged = true
		}
		if e.InProgress != "" {
			merged.InProgress = e.InProgress
		}
	}
	return merged
}

// attempts to add any in-progress .git paths that didn't exist when Start was called
func (w *Watcher) refreshInProgressPaths() {
	gitDir := filepath.Join(w.repoPath, ".git")

	candidates := []string{
		filepath.Join(gitDir, "MERGE_HEAD"),
		filepath.Join(gitDir, "CHERRY_PICK_HEAD"),
		filepath.Join(gitDir, "REVERT_HEAD"),
		filepath.Join(gitDir, "rebase-merge"),
		filepath.Join(gitDir, "rebase-apply"),
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			_ = w.fsw.Add(path)
		}
	}
}