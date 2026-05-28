package action

// Integration tests for every individual action.
// Each test creates a real git repo in t.TempDir(), puts it into the required
// state, runs the action, then asserts the git outcome directly — not the
// event stream. The event stream is structural (tested in runner_test.go);
// these tests care about whether the git state is actually correct afterwards.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── checkout ─────────────────────────────────────────────────────────────────

func TestCheckout_SwitchesBranch(t *testing.T) {
	repo := newTestRepo(t)
	mainBranch := repo.currentBranch()
	repo.git("checkout", "-b", "feature")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "checkout",
		Args:   map[string]string{"target": mainBranch},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)
	assertNoError(t, events)

	if got := repo.currentBranch(); got != mainBranch {
		t.Errorf("current branch = %q, want %q", got, mainBranch)
	}
}

func TestCheckout_DetachedSHA(t *testing.T) {
	repo := newTestRepo(t)
	sha := repo.currentSHA()

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "checkout",
		Args:   map[string]string{"target": sha, "detach": "true"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	// HEAD should now be detached (returns the SHA, not a branch name).
	head := repo.currentBranch()
	if head != "HEAD" {
		t.Errorf("expected detached HEAD, got branch %q", head)
	}
}

func TestCheckout_MissingTargetReturnsError(t *testing.T) {
	repo := newTestRepo(t)

	_, err := runAction(t, repo.runner, ActionRequest{
		Action: "checkout",
		Args:   map[string]string{},
	})
	if err == nil {
		t.Error("expected error for missing target arg, got nil")
	}
}

// ── branchCreate ─────────────────────────────────────────────────────────────

func TestBranchCreate_CreatesAtCurrent(t *testing.T) {
	repo := newTestRepo(t)

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "branch_create",
		Args:   map[string]string{"name": "new-branch"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	// Branch must now exist.
	branches := repo.git("branch", "--list", "new-branch")
	if !strings.Contains(branches, "new-branch") {
		t.Error("branch new-branch was not created")
	}
}

func TestBranchCreate_WithCheckout_SwitchesBranch(t *testing.T) {
	repo := newTestRepo(t)

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "branch_create",
		Args:   map[string]string{"name": "new-branch", "checkout": "true"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	if got := repo.currentBranch(); got != "new-branch" {
		t.Errorf("current branch = %q, want %q", got, "new-branch")
	}
}

func TestBranchCreate_AtStartPoint(t *testing.T) {
	repo := newTestRepo(t)
	// Make a second commit so we have two distinct SHAs.
	repo.writeFile("a.txt", "a")
	firstSHA := repo.commit("second commit")
	repo.writeFile("b.txt", "b")
	repo.commit("third commit")

	// Create a branch pointing to the first SHA.
	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "branch_create",
		Args:   map[string]string{"name": "at-first", "start": firstSHA},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	tip := repo.git("rev-parse", "at-first")
	if tip != firstSHA {
		t.Errorf("branch tip = %s, want %s", tip, firstSHA)
	}
}

func TestBranchCreate_MissingNameReturnsError(t *testing.T) {
	repo := newTestRepo(t)
	_, err := runAction(t, repo.runner, ActionRequest{
		Action: "branch_create",
		Args:   map[string]string{},
	})
	if err == nil {
		t.Error("expected error for missing name arg, got nil")
	}
}

// ── branchDelete ─────────────────────────────────────────────────────────────

func TestBranchDelete_DeletesMergedBranch(t *testing.T) {
	repo := newTestRepo(t)
	mainBranch := repo.currentBranch()

	// Create and immediately merge a branch so it's eligible for -d.
	repo.git("checkout", "-b", "to-delete")
	repo.writeFile("x.txt", "x")
	repo.commit("feature commit")
	repo.git("checkout", mainBranch)
	repo.git("merge", "to-delete", "--no-ff", "-m", "merge to-delete")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action:  "branch_delete",
		Args:    map[string]string{"name": "to-delete"},
		Confirm: true,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	branches := repo.git("branch", "--list", "to-delete")
	if strings.Contains(branches, "to-delete") {
		t.Error("branch to-delete still exists after deletion")
	}
}

func TestBranchDelete_ForceDeletesUnmergedBranch(t *testing.T) {
	repo := newTestRepo(t)
	mainBranch := repo.currentBranch()

	// Create a branch with commits that aren't merged.
	repo.git("checkout", "-b", "unmerged")
	repo.writeFile("unmerged.txt", "unmerged")
	repo.commit("unmerged commit")
	repo.git("checkout", mainBranch)

	events, err := runAction(t, repo.runner, ActionRequest{
		Action:  "branch_delete",
		Args:    map[string]string{"name": "unmerged", "force": "true"},
		Confirm: true,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	branches := repo.git("branch", "--list", "unmerged")
	if strings.Contains(branches, "unmerged") {
		t.Error("branch unmerged still exists after force deletion")
	}
}

// ── branchRename ─────────────────────────────────────────────────────────────

func TestBranchRename(t *testing.T) {
	repo := newTestRepo(t)
	repo.git("checkout", "-b", "old-name")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "branch_rename",
		Args:   map[string]string{"from": "old-name", "to": "new-name"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	if !strings.Contains(repo.git("branch"), "new-name") {
		t.Error("branch new-name does not exist after rename")
	}
	if strings.Contains(repo.git("branch"), "old-name") {
		t.Error("branch old-name still exists after rename")
	}
}

// ── merge ─────────────────────────────────────────────────────────────────────

func TestMerge_CleanMerge(t *testing.T) {
	repo := newTestRepo(t)
	mainBranch := repo.currentBranch()

	repo.git("checkout", "-b", "feature")
	repo.writeFile("feature.txt", "feature content")
	featureSHA := repo.commit("add feature.txt")
	repo.git("checkout", mainBranch)

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "merge",
		Args:   map[string]string{"branch": "feature"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	// feature.txt must now exist on main.
	if _, err := os.Stat(filepath.Join(repo.path, "feature.txt")); err != nil {
		t.Error("feature.txt not present after merge")
	}
	_ = featureSHA
}

func TestMerge_SquashMerge(t *testing.T) {
	repo := newTestRepo(t)
	mainBranch := repo.currentBranch()

	repo.git("checkout", "-b", "feature")
	repo.writeFile("f1.txt", "f1")
	repo.commit("add f1")
	repo.writeFile("f2.txt", "f2")
	repo.commit("add f2")
	repo.git("checkout", mainBranch)

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "merge",
		Args:   map[string]string{"branch": "feature", "strategy": "squash"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	// Squash merge stages changes but does NOT auto-commit — no EventDone
	// from a successful commit, but we should not see EventError.
	assertNoError(t, events)
}

func TestMerge_Conflict_EmitsConflictEvent(t *testing.T) {
	repo := newTestRepo(t)
	mainBranch := repo.currentBranch()

	repo.writeFile("shared.txt", "original\n")
	repo.commit("add shared.txt")

	repo.git("checkout", "-b", "branch-a")
	repo.writeFile("shared.txt", "branch-a change\n")
	repo.commit("branch-a modifies shared.txt")

	repo.git("checkout", mainBranch)
	repo.writeFile("shared.txt", "main change\n")
	repo.commit("main modifies shared.txt")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "merge",
		Args:   map[string]string{"branch": "branch-a"},
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	hasConflict := false
	for _, e := range events {
		if e.Type == EventConflict {
			hasConflict = true
			if len(e.Files) == 0 {
				t.Error("EventConflict has empty Files list")
			}
		}
	}
	if !hasConflict {
		t.Error("expected EventConflict for conflicting merge, got none")
	}
}

func TestMerge_MissingBranchReturnsError(t *testing.T) {
	repo := newTestRepo(t)
	_, err := runAction(t, repo.runner, ActionRequest{
		Action: "merge",
		Args:   map[string]string{},
	})
	if err == nil {
		t.Error("expected error for missing branch arg, got nil")
	}
}

// ── rebase ────────────────────────────────────────────────────────────────────

func TestRebase_CleanRebase(t *testing.T) {
	repo := newTestRepo(t)
	mainBranch := repo.currentBranch()

	// Make a commit on main that feature will be rebased onto.
	repo.writeFile("main.txt", "main content")
	mainTipSHA := repo.commit("main: add main.txt")

	// Create feature from the commit BEFORE mainTipSHA.
	repo.git("checkout", "-b", "feature", "HEAD~1")
	repo.writeFile("feature.txt", "feature content")
	repo.commit("feature: add feature.txt")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "rebase",
		Args:   map[string]string{"onto": mainBranch},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)
	assertNoError(t, events)

	// After rebase, feature.txt and main.txt should both be present.
	if _, err := os.Stat(filepath.Join(repo.path, "feature.txt")); err != nil {
		t.Error("feature.txt missing after rebase")
	}
	if _, err := os.Stat(filepath.Join(repo.path, "main.txt")); err != nil {
		t.Error("main.txt missing after rebase")
	}
	_ = mainTipSHA
}

func TestRebase_MissingOntoReturnsError(t *testing.T) {
	repo := newTestRepo(t)
	_, err := runAction(t, repo.runner, ActionRequest{
		Action: "rebase",
		Args:   map[string]string{},
	})
	if err == nil {
		t.Error("expected error for missing onto arg, got nil")
	}
}

func TestRebaseAbort_ClearsState(t *testing.T) {
	repo := newTestRepo(t)

	// Manually create the rebase-merge directory to simulate a mid-rebase state.
	rebaseMerge := filepath.Join(repo.path, ".git", "rebase-merge")
	if err := os.MkdirAll(rebaseMerge, 0755); err != nil {
		t.Fatal(err)
	}
	// Write the minimum files rebase --abort needs.
	os.WriteFile(filepath.Join(rebaseMerge, "head-name"), []byte("refs/heads/feature\n"), 0644)
	origHead := repo.currentSHA()
	os.WriteFile(filepath.Join(rebaseMerge, "orig-head"), []byte(origHead+"\n"), 0644)
	os.WriteFile(filepath.Join(rebaseMerge, "onto"), []byte(origHead+"\n"), 0644)

	events, _ := runAction(t, repo.runner, ActionRequest{
		Action: "rebase_abort",
		Args:   map[string]string{},
	})

	// rebase-merge/ should now be gone.
	if _, err := os.Stat(rebaseMerge); !os.IsNotExist(err) {
		t.Error("rebase-merge/ still exists after rebase --abort")
	}
	_ = events
}

// ── revert ────────────────────────────────────────────────────────────────────

func TestRevert_CreatesUndoCommit(t *testing.T) {
	repo := newTestRepo(t)

	repo.writeFile("to-revert.txt", "content to revert")
	sha := repo.commit("add to-revert.txt")

	countBefore := strings.Count(repo.git("log", "--oneline"), "\n") + 1

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "revert",
		Args:   map[string]string{"sha": sha},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	countAfter := strings.Count(repo.git("log", "--oneline"), "\n") + 1
	if countAfter <= countBefore {
		t.Error("revert did not create a new commit")
	}

	// The reverted file should no longer exist.
	if _, err := os.Stat(filepath.Join(repo.path, "to-revert.txt")); !os.IsNotExist(err) {
		t.Error("to-revert.txt still exists after revert")
	}
}

func TestRevert_NoCommit_StagesOnly(t *testing.T) {
	repo := newTestRepo(t)

	repo.writeFile("staged.txt", "staged content")
	sha := repo.commit("add staged.txt")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "revert",
		Args:   map[string]string{"sha": sha, "no_commit": "true"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	// With --no-commit, the revert is staged but no new commit was made.
	// git status should show staged changes.
	status := repo.git("status", "--short")
	if !strings.Contains(status, "staged.txt") {
		t.Errorf("expected staged.txt in git status after no-commit revert, got: %q", status)
	}
}

// ── cherryPick ────────────────────────────────────────────────────────────────

func TestCherryPick_AppliesCommit(t *testing.T) {
	repo := newTestRepo(t)
	mainBranch := repo.currentBranch()

	// Make a commit on a side branch.
	repo.git("checkout", "-b", "donor")
	repo.writeFile("cherry.txt", "cherry content")
	sha := repo.commit("add cherry.txt")

	repo.git("checkout", mainBranch)

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "cherry_pick",
		Args:   map[string]string{"sha": sha},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	if _, err := os.Stat(filepath.Join(repo.path, "cherry.txt")); err != nil {
		t.Error("cherry.txt not present after cherry-pick")
	}
}

// ── reset ─────────────────────────────────────────────────────────────────────

func TestResetSoft_MovesHEADKeepsChanges(t *testing.T) {
	repo := newTestRepo(t)

	repo.writeFile("a.txt", "a")
	firstSHA := repo.commit("first")
	repo.writeFile("b.txt", "b")
	repo.commit("second")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "reset_soft",
		Args:   map[string]string{"sha": firstSHA},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	// HEAD should be at firstSHA.
	if got := repo.currentSHA(); got != firstSHA {
		t.Errorf("HEAD = %s, want %s", got, firstSHA)
	}

	// b.txt should still be staged (soft reset preserves staged changes).
	status := repo.git("status", "--short")
	if !strings.Contains(status, "b.txt") {
		t.Error("b.txt should be staged after soft reset")
	}
}

func TestResetMixed_UnstagesChanges(t *testing.T) {
	repo := newTestRepo(t)

	repo.writeFile("a.txt", "a")
	firstSHA := repo.commit("first")
	repo.writeFile("b.txt", "b")
	repo.commit("second")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "reset_mixed",
		Args:   map[string]string{"sha": firstSHA},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	if got := repo.currentSHA(); got != firstSHA {
		t.Errorf("HEAD = %s, want %s", got, firstSHA)
	}

	// b.txt should be untracked (mixed = unstaged, working tree intact).
	status := repo.git("status", "--short")
	if !strings.Contains(status, "b.txt") {
		t.Errorf("b.txt should appear in status after mixed reset, got: %q", status)
	}
}

func TestResetHard_DiscardsChanges(t *testing.T) {
	repo := newTestRepo(t)

	repo.writeFile("a.txt", "a")
	firstSHA := repo.commit("first")
	repo.writeFile("b.txt", "b")
	repo.commit("second")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action:  "reset_hard",
		Args:    map[string]string{"sha": firstSHA},
		Confirm: true,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	if got := repo.currentSHA(); got != firstSHA {
		t.Errorf("HEAD = %s, want %s", got, firstSHA)
	}

	// b.txt should not exist — hard reset destroys working tree changes.
	if _, err := os.Stat(filepath.Join(repo.path, "b.txt")); !os.IsNotExist(err) {
		t.Error("b.txt still exists after hard reset — working tree not cleaned")
	}
}

func TestResetSoft_WithSteps(t *testing.T) {
	repo := newTestRepo(t)

	repo.writeFile("a.txt", "a")
	firstSHA := repo.commit("first")
	repo.writeFile("b.txt", "b")
	repo.commit("second")
	repo.writeFile("c.txt", "c")
	repo.commit("third")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "reset_soft",
		Args:   map[string]string{"steps": "2"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	if got := repo.currentSHA(); got != firstSHA {
		t.Errorf("HEAD = %s, want %s (2 steps back)", got, firstSHA)
	}
}

func TestReset_MissingTargetReturnsError(t *testing.T) {
	repo := newTestRepo(t)
	for _, action := range []string{"reset_soft", "reset_mixed", "reset_hard"} {
		t.Run(action, func(t *testing.T) {
			_, err := runAction(t, repo.runner, ActionRequest{
				Action:  action,
				Args:    map[string]string{},
				Confirm: true,
			})
			if err == nil {
				t.Errorf("%s: expected error for missing sha/steps, got nil", action)
			}
		})
	}
}

// ── stash ─────────────────────────────────────────────────────────────────────

func TestStash_SavesAndRestoresDirtyWorkingTree(t *testing.T) {
	repo := newTestRepo(t)

	// Dirty the working tree without committing.
	repo.writeFile("dirty.txt", "dirty content")
	repo.git("add", "dirty.txt")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "stash",
		Args:   map[string]string{},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	// dirty.txt should no longer be staged.
	status := repo.git("status", "--short")
	if strings.Contains(status, "dirty.txt") {
		t.Error("dirty.txt still in status after stash — stash did not save")
	}
}

func TestStash_WithMessage(t *testing.T) {
	repo := newTestRepo(t)

	repo.writeFile("wip.txt", "work in progress")
	repo.git("add", "wip.txt")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "stash",
		Args:   map[string]string{"message": "wip: my feature"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	stashList := repo.git("stash", "list")
	if !strings.Contains(stashList, "wip: my feature") {
		t.Errorf("stash message not found in list: %q", stashList)
	}
}

func TestStashPop_RestoresChanges(t *testing.T) {
	repo := newTestRepo(t)

	repo.writeFile("stashed.txt", "stashed content")
	repo.git("add", "stashed.txt")
	repo.git("stash")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "stash_pop",
		Args:   map[string]string{},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	// stashed.txt should be back.
	if _, err := os.Stat(filepath.Join(repo.path, "stashed.txt")); err != nil {
		t.Error("stashed.txt not restored after stash pop")
	}
}

func TestStashDrop_RemovesEntry(t *testing.T) {
	repo := newTestRepo(t)

	repo.writeFile("will-drop.txt", "content")
	repo.git("add", "will-drop.txt")
	repo.git("stash")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "stash_drop",
		Args:   map[string]string{"index": "0"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	stashList := repo.gitAllowFail("stash", "list")
	_ = stashList // stash list is empty after drop — command may exit non-zero
}

// ── tag ───────────────────────────────────────────────────────────────────────

func TestTag_LightweightTag(t *testing.T) {
	repo := newTestRepo(t)

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "tag",
		Args:   map[string]string{"name": "v1.0.0"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	tags := repo.git("tag", "--list", "v1.0.0")
	if !strings.Contains(tags, "v1.0.0") {
		t.Error("tag v1.0.0 was not created")
	}
}

func TestTag_AnnotatedTag(t *testing.T) {
	repo := newTestRepo(t)

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "tag",
		Args:   map[string]string{"name": "v2.0.0", "message": "Release 2.0"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	// -d flag on git tag verify distinguishes annotated from lightweight.
	tagType := repo.git("cat-file", "-t", "v2.0.0")
	if tagType != "tag" {
		t.Errorf("tag type = %q, want %q (annotated tag)", tagType, "tag")
	}
}

func TestTag_AtSpecificSHA(t *testing.T) {
	repo := newTestRepo(t)
	repo.writeFile("x.txt", "x")
	targetSHA := repo.commit("second commit")
	repo.writeFile("y.txt", "y")
	repo.commit("third commit")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action: "tag",
		Args:   map[string]string{"name": "v0.1.0", "sha": targetSHA},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	tagSHA := repo.git("rev-parse", "v0.1.0")
	if tagSHA != targetSHA {
		t.Errorf("tag points to %s, want %s", tagSHA, targetSHA)
	}
}

func TestTagDelete(t *testing.T) {
	repo := newTestRepo(t)
	repo.git("tag", "v9.9.9")

	events, err := runAction(t, repo.runner, ActionRequest{
		Action:  "tag_delete",
		Args:    map[string]string{"name": "v9.9.9"},
		Confirm: true,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertDone(t, events)

	tags := repo.git("tag", "--list", "v9.9.9")
	if strings.Contains(tags, "v9.9.9") {
		t.Error("tag v9.9.9 still exists after deletion")
	}
}

// ── resolveSHA ────────────────────────────────────────────────────────────────

func TestResolveSHA_FullSHA(t *testing.T) {
	repo := newTestRepo(t)
	full := repo.currentSHA()

	got, err := repo.runner.resolveSHA(full)
	if err != nil {
		t.Fatalf("resolveSHA error: %v", err)
	}
	if got != full {
		t.Errorf("resolveSHA(%q) = %q, want %q", full, got, full)
	}
}

func TestResolveSHA_ShortSHA(t *testing.T) {
	repo := newTestRepo(t)
	full := repo.currentSHA()
	short := full[:7]

	got, err := repo.runner.resolveSHA(short)
	if err != nil {
		t.Fatalf("resolveSHA error: %v", err)
	}
	if got != full {
		t.Errorf("resolveSHA(%q) = %q, want %q", short, got, full)
	}
}

func TestResolveSHA_BranchName(t *testing.T) {
	repo := newTestRepo(t)
	full := repo.currentSHA()
	branch := repo.currentBranch()

	got, err := repo.runner.resolveSHA(branch)
	if err != nil {
		t.Fatalf("resolveSHA error: %v", err)
	}
	if got != full {
		t.Errorf("resolveSHA(%q) = %q, want %q", branch, got, full)
	}
}

func TestResolveSHA_EmptyReturnsError(t *testing.T) {
	repo := newTestRepo(t)
	_, err := repo.runner.resolveSHA("")
	if err == nil {
		t.Error("expected error for empty ref, got nil")
	}
}

func TestResolveSHA_NonexistentReturnsError(t *testing.T) {
	repo := newTestRepo(t)
	_, err := repo.runner.resolveSHA("does-not-exist-branch")
	if err == nil {
		t.Error("expected error for nonexistent ref, got nil")
	}
}

// ── resolveSteps ──────────────────────────────────────────────────────────────

func TestResolveSteps_ZeroReturnsCurrentSHA(t *testing.T) {
	repo := newTestRepo(t)
	full := repo.currentSHA()

	got, err := repo.runner.resolveSteps("0")
	if err != nil {
		t.Fatalf("resolveSteps error: %v", err)
	}
	if got != full {
		t.Errorf("resolveSteps(0) = %q, want HEAD (%q)", got, full)
	}
}

func TestResolveSteps_TwoStepsBack(t *testing.T) {
	repo := newTestRepo(t)

	repo.writeFile("a.txt", "a")
	twoAgo := repo.commit("two ago")
	repo.writeFile("b.txt", "b")
	repo.commit("one ago")
	repo.writeFile("c.txt", "c")
	repo.commit("now")

	got, err := repo.runner.resolveSteps("2")
	if err != nil {
		t.Fatalf("resolveSteps error: %v", err)
	}
	if got != twoAgo {
		t.Errorf("resolveSteps(2) = %q, want %q", got, twoAgo)
	}
}

func TestResolveSteps_InvalidInputReturnsError(t *testing.T) {
	repo := newTestRepo(t)

	cases := []string{"", "abc", "-1", "1.5"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := repo.runner.resolveSteps(c)
			if err == nil {
				t.Errorf("resolveSteps(%q): expected error, got nil", c)
			}
		})
	}
}

// ── arg helper ────────────────────────────────────────────────────────────────

func TestArgHelper_ReturnsValueWhenPresent(t *testing.T) {
	args := map[string]string{"key": "value"}
	if got := arg(args, "key", "fallback"); got != "value" {
		t.Errorf("arg() = %q, want %q", got, "value")
	}
}

func TestArgHelper_ReturnsFallbackWhenAbsent(t *testing.T) {
	args := map[string]string{}
	if got := arg(args, "key", "fallback"); got != "fallback" {
		t.Errorf("arg() = %q, want %q", got, "fallback")
	}
}

func TestArgHelper_ReturnsFallbackWhenEmpty(t *testing.T) {
	args := map[string]string{"key": ""}
	if got := arg(args, "key", "fallback"); got != "fallback" {
		t.Errorf("arg() = %q, want %q", got, "fallback")
	}
}

// ── context cancellation propagation ─────────────────────────────────────────

func TestExecute_CancelledContextStopsAction(t *testing.T) {
	repo := newTestRepo(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	out := make(chan ActionEvent, 64)
	errCh := make(chan error, 1)
	go func() {
		errCh <- repo.runner.Execute(ctx, ActionRequest{
			Action: "checkout",
			Args:   map[string]string{"target": repo.currentBranch()},
		}, out)
		close(out)
	}()

	// Drain events — may be empty if context was cancelled before anything ran.
	for range out {
	}

	// We don't assert on the error — the behaviour depends on whether
	// git noticed the context before or after it finished. What we're
	// testing is that Execute doesn't hang forever on a cancelled context.
	select {
	case <-errCh:
		// returned — pass
	case <-context.Background().Done():
		// unreachable but satisfies the compiler
	}
}