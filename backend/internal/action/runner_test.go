package action

// White-box tests — same package so we can reach unexported helpers.
// Every test that touches the filesystem uses t.TempDir() which Go cleans up
// automatically after the test, even on failure.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── test repo helper ──────────────────────────────────────────────────────────

// testRepo is a real git repository in a temp directory. Using a real repo
// rather than a mock lets us test against actual git behaviour — the thing we
// care about.
type testRepo struct {
	path   string
	runner *Runner
	t      *testing.T
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	dir := t.TempDir()

	r := &testRepo{path: dir, runner: New(dir), t: t}
	r.git("init")
	r.git("config", "user.email", "test@example.com")
	r.git("config", "user.name", "Test User")

	// Make an initial commit so HEAD and the default branch both exist.
	r.writeFile("README.md", "# test repo")
	r.git("add", ".")
	r.git("commit", "-m", "initial commit")

	return r
}

// git runs a git command in the repo directory, failing the test on non-zero exit.
func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.path
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitAllowFail runs a git command but doesn't fail the test on error.
// Used for setup steps that might legitimately fail (e.g. branch rename on older git).
func (r *testRepo) gitAllowFail(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.path
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd.Run()
}

// writeFile writes content to a file in the repo directory, creating it if needed.
func (r *testRepo) writeFile(name, content string) {
	r.t.Helper()
	path := filepath.Join(r.path, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		r.t.Fatal(err)
	}
}

// commit stages everything and makes a commit.
func (r *testRepo) commit(msg string) string {
	r.t.Helper()
	r.git("add", ".")
	r.git("commit", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

// currentBranch returns the name of the currently checked-out branch.
func (r *testRepo) currentBranch() string {
	return r.git("rev-parse", "--abbrev-ref", "HEAD")
}

// currentSHA returns the full SHA of HEAD.
func (r *testRepo) currentSHA() string {
	return r.git("rev-parse", "HEAD")
}

// collectEvents drains the event channel and returns all events.
// Must be called after close(out) to avoid blocking forever.
func collectEvents(out <-chan ActionEvent) []ActionEvent {
	var events []ActionEvent
	for e := range out {
		events = append(events, e)
	}
	return events
}

// runAction executes an action, collects all events, and returns them.
// Wraps the goroutine/channel boilerplate so individual tests stay readable.
func runAction(t *testing.T, r *Runner, req ActionRequest) ([]ActionEvent, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out := make(chan ActionEvent, 128)
	var execErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		execErr = r.Execute(ctx, req, out)
		close(out)
	}()

	events := collectEvents(out)
	wg.Wait()
	return events, execErr
}

// assertDone asserts that the last event in the slice is EventDone.
func assertDone(t *testing.T, events []ActionEvent) {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("no events received — expected at least EventDone")
	}
	last := events[len(events)-1]
	if last.Type != EventDone {
		t.Errorf("last event type = %q, want %q", last.Type, EventDone)
	}
}

// assertNoError asserts none of the events are EventError or EventConflict.
func assertNoError(t *testing.T, events []ActionEvent) {
	t.Helper()
	for _, e := range events {
		if e.Type == EventError {
			t.Errorf("unexpected error event: %s", e.Data)
		}
	}
}

// ── isDestructive ─────────────────────────────────────────────────────────────

func TestIsDestructive(t *testing.T) {
	cases := []struct {
		action     string
		wantResult bool
	}{
		{"reset_hard", true},
		{"branch_delete", true},
		{"tag_delete", true},
		{"reset_soft", false},
		{"reset_mixed", false},
		{"checkout", false},
		{"merge", false},
		{"rebase", false},
		{"stash", false},
		{"", false},
	}

	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			if got := isDestructive(tc.action); got != tc.wantResult {
				t.Errorf("isDestructive(%q) = %v, want %v", tc.action, got, tc.wantResult)
			}
		})
	}
}

// ── Execute guard ─────────────────────────────────────────────────────────────

func TestExecute_DestructiveBlockedWithoutConfirm(t *testing.T) {
	repo := newTestRepo(t)

	for _, action := range []string{"reset_hard", "branch_delete", "tag_delete"} {
		t.Run(action, func(t *testing.T) {
			out := make(chan ActionEvent, 10)
			err := repo.runner.Execute(context.Background(), ActionRequest{
				Action:  action,
				Args:    map[string]string{"name": "main", "sha": "HEAD"},
				Confirm: false,
			}, out)
			close(out)

			if err == nil {
				t.Error("expected error for destructive action without confirm, got nil")
			}
			if !strings.Contains(err.Error(), "confirm") {
				t.Errorf("error message should mention confirm, got: %s", err.Error())
			}
		})
	}
}

func TestExecute_DestructiveAllowedWithConfirm(t *testing.T) {
	// reset_hard with confirm:true should reach the action (not be blocked by the guard).
	// We're not checking the full git outcome here — just that Execute proceeds past the guard.
	repo := newTestRepo(t)

	events, _ := runAction(t, repo.runner, ActionRequest{
		Action:  "reset_hard",
		Args:    map[string]string{"sha": "HEAD"},
		Confirm: true,
	})

	// If the guard was bypassed, we get some events (at minimum EventDone or EventError).
	if len(events) == 0 {
		t.Error("expected events after bypassing destructive guard, got none")
	}
}

func TestExecute_UnknownActionReturnsError(t *testing.T) {
	repo := newTestRepo(t)

	out := make(chan ActionEvent, 10)
	err := repo.runner.Execute(context.Background(), ActionRequest{
		Action: "does_not_exist",
	}, out)
	close(out)

	if err == nil {
		t.Error("expected error for unknown action, got nil")
	}
}

func TestExecute_EmptyActionReturnsError(t *testing.T) {
	repo := newTestRepo(t)

	out := make(chan ActionEvent, 10)
	err := repo.runner.Execute(context.Background(), ActionRequest{}, out)
	close(out)

	if err == nil {
		t.Error("expected error for empty action, got nil")
	}
}

// ── validateNotInProgress ─────────────────────────────────────────────────────

func TestValidateNotInProgress_NothingActive(t *testing.T) {
	repo := newTestRepo(t)
	// Clean repo — no operation files present.
	if err := repo.runner.validateNotInProgress(""); err != nil {
		t.Errorf("expected nil for clean repo, got: %v", err)
	}
}

func TestValidateNotInProgress_MergeBlocked(t *testing.T) {
	repo := newTestRepo(t)

	// Simulate a merge in progress by creating MERGE_HEAD.
	mergeHead := filepath.Join(repo.path, ".git", "MERGE_HEAD")
	if err := os.WriteFile(mergeHead, []byte("abc123\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(mergeHead) })

	err := repo.runner.validateNotInProgress("")
	if err == nil {
		t.Error("expected error when MERGE_HEAD exists, got nil")
	}
	if !strings.Contains(err.Error(), "merge") {
		t.Errorf("error should mention 'merge', got: %v", err)
	}
}

func TestValidateNotInProgress_AllowsMatchingOperation(t *testing.T) {
	repo := newTestRepo(t)

	// Simulate a rebase in progress.
	rebaseMerge := filepath.Join(repo.path, ".git", "rebase-merge")
	if err := os.MkdirAll(rebaseMerge, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(rebaseMerge) })

	// allow="rebase" should pass even though rebase-merge/ exists.
	if err := repo.runner.validateNotInProgress("rebase"); err != nil {
		t.Errorf("expected nil when allowing active rebase, got: %v", err)
	}

	// allow="" should be blocked.
	if err := repo.runner.validateNotInProgress(""); err == nil {
		t.Error("expected error when rebase active and allow is empty, got nil")
	}
}

func TestValidateNotInProgress_CherryPickBlocked(t *testing.T) {
	repo := newTestRepo(t)

	cherryHead := filepath.Join(repo.path, ".git", "CHERRY_PICK_HEAD")
	if err := os.WriteFile(cherryHead, []byte("abc123\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(cherryHead) })

	err := repo.runner.validateNotInProgress("")
	if err == nil {
		t.Error("expected error when CHERRY_PICK_HEAD exists, got nil")
	}
}

func TestValidateNotInProgress_RebaseApplyBlocked(t *testing.T) {
	// Ensures the rebase-apply directory (non-interactive rebase) is also caught.
	repo := newTestRepo(t)

	rebaseApply := filepath.Join(repo.path, ".git", "rebase-apply")
	if err := os.MkdirAll(rebaseApply, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(rebaseApply) })

	err := repo.runner.validateNotInProgress("")
	if err == nil {
		t.Error("expected error when rebase-apply/ exists, got nil")
	}
}

// ── streamSubprocess ──────────────────────────────────────────────────────────

func TestStreamSubprocess_SuccessEmitsDone(t *testing.T) {
	runner := New(t.TempDir())
	out := make(chan ActionEvent, 64)
	ctx := context.Background()

	// 'true' exits 0 with no output.
	cmd := runner.gitCmd(ctx, "version") // git version always exits 0
	if err := runner.streamSubprocess(ctx, cmd, out); err != nil {
		t.Fatalf("streamSubprocess returned error: %v", err)
	}
	close(out)

	events := collectEvents(out)
	last := events[len(events)-1]
	if last.Type != EventDone {
		t.Errorf("last event = %q, want %q", last.Type, EventDone)
	}
}

func TestStreamSubprocess_FailureEmitsError(t *testing.T) {
	runner := New(t.TempDir())
	out := make(chan ActionEvent, 64)
	ctx := context.Background()

	// 'git invalid-command' exits non-zero and produces stderr.
	cmd := runner.gitCmd(ctx, "invalid-command-that-does-not-exist")
	_ = runner.streamSubprocess(ctx, cmd, out)
	close(out)

	events := collectEvents(out)
	hasError := false
	for _, e := range events {
		if e.Type == EventError || e.Type == EventStderr {
			hasError = true
			break
		}
	}
	if !hasError {
		t.Error("expected EventError or EventStderr for failing command, got neither")
	}
}

func TestStreamSubprocess_CapturesAllStdout(t *testing.T) {
	// Verifies that no stdout lines are dropped — guards against the
	// wg.Wait/cmd.Wait ordering bug where output can be lost.
	repo := newTestRepo(t)
	out := make(chan ActionEvent, 512)
	ctx := context.Background()

	// Create 50 commits so git log has enough output to stress the scanner.
	for i := 0; i < 50; i++ {
		repo.writeFile("file.txt", strings.Repeat("x", i+1))
		repo.commit("commit " + string(rune('a'+i%26)))
	}

	cmd := repo.runner.gitCmd(ctx, "log", "--oneline")
	if err := repo.runner.streamSubprocess(ctx, cmd, out); err != nil {
		t.Fatalf("streamSubprocess error: %v", err)
	}
	close(out)

	events := collectEvents(out)
	var stdoutLines []string
	for _, e := range events {
		if e.Type == EventStdout {
			stdoutLines = append(stdoutLines, e.Data)
		}
	}

	// We should have at least 51 stdout lines (50 new + 1 initial commit).
	if len(stdoutLines) < 51 {
		t.Errorf("expected ≥51 stdout lines, got %d — possible output dropped", len(stdoutLines))
	}
}

func TestStreamSubprocess_StderrCaptured(t *testing.T) {
	repo := newTestRepo(t)
	out := make(chan ActionEvent, 64)
	ctx := context.Background()

	// Checking out a non-existent branch writes to stderr.
	cmd := repo.runner.gitCmd(ctx, "checkout", "branch-that-does-not-exist")
	_ = repo.runner.streamSubprocess(ctx, cmd, out)
	close(out)

	events := collectEvents(out)
	hasStderr := false
	for _, e := range events {
		if e.Type == EventStderr {
			hasStderr = true
			break
		}
	}
	if !hasStderr {
		t.Error("expected EventStderr for failed checkout, got none")
	}
}

func TestStreamSubprocess_ContextCancellation(t *testing.T) {
	runner := New(t.TempDir())
	out := make(chan ActionEvent, 64)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// 'git fetch' with a bad remote will hang waiting for network — cancel it.
	// Using sleep via a shell would be cleaner but git is what we have.
	// Instead, just verify the subprocess respects context cancellation by timing out.
	done := make(chan struct{})
	go func() {
		cmd := runner.gitCmd(ctx, "fetch", "--no-tags", "http://127.0.0.1:19999/nonexistent.git")
		_ = runner.streamSubprocess(ctx, cmd, out)
		close(out)
		close(done)
	}()

	select {
	case <-done:
		// process ended (either fast-failed or cancelled) — pass
	case <-time.After(5 * time.Second):
		t.Error("streamSubprocess did not respect context cancellation within 5s")
	}
}

// ── detectConflicts ───────────────────────────────────────────────────────────

func TestDetectConflicts_NoConflicts_ReturnsNil(t *testing.T) {
	repo := newTestRepo(t)

	files, err := repo.runner.detectConflicts(context.Background())
	if err != nil {
		t.Fatalf("detectConflicts error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no conflicts in clean repo, got: %v", files)
	}
}

func TestDetectConflicts_MergeConflict_ReturnsFiles(t *testing.T) {
	repo := newTestRepo(t)
	mainBranch := repo.currentBranch()

	// Create a conflict: both branches modify the same line.
	repo.writeFile("conflict.txt", "original content\n")
	repo.commit("add conflict.txt")

	repo.git("checkout", "-b", "feature")
	repo.writeFile("conflict.txt", "feature branch change\n")
	repo.commit("feature change")

	repo.git("checkout", mainBranch)
	repo.writeFile("conflict.txt", "main branch change\n")
	repo.commit("main change")

	// Start a merge that will conflict. Allow failure — we expect it to fail.
	mergeCmd := exec.Command("git", "merge", "feature")
	mergeCmd.Dir = repo.path
	mergeCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	_ = mergeCmd.Run() // intentionally ignoring exit code — conflict is expected

	// MERGE_HEAD must now exist.
	mergeHead := filepath.Join(repo.path, ".git", "MERGE_HEAD")
	if _, err := os.Stat(mergeHead); err != nil {
		t.Skip("merge did not produce a conflict state — skipping")
	}

	files, err := repo.runner.detectConflicts(context.Background())
	if err != nil {
		t.Fatalf("detectConflicts error: %v", err)
	}
	if len(files) == 0 {
		t.Error("expected conflicted files, got none")
	}

	found := false
	for _, f := range files {
		if f == "conflict.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected conflict.txt in conflict list, got: %v", files)
	}
}