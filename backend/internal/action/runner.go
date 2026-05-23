package action
 
import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)
 
type ActionRequest struct {
	Action  string            `json:"action"`
	Args    map[string]string `json:"args"`
	Confirm bool              `json:"confirm"` // required true for destructive ops
}
 
type EventType string
 
const (
	EventStdout   EventType = "stdout"
	EventStderr   EventType = "stderr"
	EventConflict EventType = "conflict"
	EventDone     EventType = "done"
	EventError    EventType = "error"
)
 
type ActionEvent struct {
	Type  EventType `json:"type"`
	Data  string    `json:"data"`
	Files []string  `json:"files,omitempty"`
}
 
// executes git operations for the repo at repoPath
type Runner struct {
	repoPath string
}
 
// create a runner for given repo
func New(repoPath string) *Runner { 
	return &Runner{
		repoPath: repoPath,
	} 
}
 
// dispatches the action and streams events to out until completion
func (r *Runner) Execute(ctx context.Context, req ActionRequest, out chan<- ActionEvent) error {
	if isDestructive(req.Action) && !req.Confirm {
		return fmt.Errorf("409: action '%s' requires confirmation", req.Action)
	}

	allow := ""
	switch req.Action {
	case "rebase_continue", "rebase_abort":
		allow = "rebase"

	case "cherry_pick_abort":
		allow = "cherry-pick"
	}

	if err := r.validateNotInProgress(allow); err != nil { return err }

	switch req.Action {
		case "checkout":
			return r.checkout(ctx, req.Args, out)

		case "branch_create":
			return r.branchCreate(ctx, req.Args, out)

		case "branch_delete":
			return r.branchDelete(ctx, req.Args, out)

		case "branch_rename":
			return r.branchRename(ctx, req.Args, out)

		case "merge":
			return r.merge(ctx, req.Args, out)

		case "rebase":
			return r.rebase(ctx, req.Args, out)

		case "rebase_abort":
			return r.rebaseAbort(ctx, req.Args, out)

		case "rebase_continue":
			return r.rebaseContinue(ctx, req.Args, out)

		case "revert":
			return r.revert(ctx, req.Args, out)

		case "cherry_pick":
			return r.cherryPick(ctx, req.Args, out)

		case "cherry_pick_abort":
			return r.cherryPickAbort(ctx, req.Args, out)

		case "reset_soft":
			return r.resetSoft(ctx, req.Args, out)

		case "reset_mixed":
			return r.resetMixed(ctx, req.Args, out)

		case "reset_hard":
			return r.resetHard(ctx, req.Args, out)

		case "stash":
			return r.stash(ctx, req.Args, out)

		case "stash_pop":
			return r.stashPop(ctx, req.Args, out)

		case "stash_drop":
			return r.stashDrop(ctx, req.Args, out)

		case "tag":
			return r.tag(ctx, req.Args, out)

		case "tag_delete":
			return r.tagDelete(ctx, req.Args, out)

		case "fetch":
			return r.fetch(ctx, req.Args, out)

		default:
			return fmt.Errorf("unknown action: %s", req.Action)
	}

}
 
// prevents accidental data destruction
func isDestructive(action string) bool { 
	switch action {
		case
			"reset_hard",
			"branch_delete",
			"tag_delete":
			return true
	}

	return false
}
 
// checks for MERGE_HEAD or rebase-merge/ and reads conflicted
// paths from the index. Returns the list of conflicted file paths.
func (r *Runner) detectConflicts() ([]string, error) { 
	gitDir := filepath.Join(r.repoPath, ".git")

	mergeHead := filepath.Join(gitDir, "MERGE_HEAD")
	rebaseMerge := filepath.Join(gitDir, "rebase-merge")

	_, mergeErr := os.Stat(mergeHead)
	_, rebaseErr := os.Stat(rebaseMerge)

	if mergeErr != nil && rebaseErr != nil { return nil, nil }

	cmd := r.gitCmd(
		context.Background(),
		"diff",
		"--name-only",
		"--diff-filter=U",
	)

	output, err := cmd.Output()
	if err != nil { return nil, err }

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line != "" {
			files = append(files, line)
		}
	}

	return files, nil
}
 
// pipes stdout & stderr to frontend, emits EventDone or EventError when the process exits
func (r *Runner) streamSubprocess(ctx context.Context, cmd *exec.Cmd, out chan<- ActionEvent) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil { return err }

	stderr, err := cmd.StderrPipe()
	if err != nil {	return err }
	if err := cmd.Start(); err != nil { return err }

	var wg sync.WaitGroup
	wg.Add(2)

	stream := func(scanner *bufio.Scanner, eventType EventType,) {
		defer wg.Done()

		for scanner.Scan() {
			select {
				case <-ctx.Done():
					return

				case out <- ActionEvent{
					Type: eventType,
					Data: scanner.Text(),
				}:
			}
		}
	}

	go stream(bufio.NewScanner(stdout), EventStdout)
	go stream(bufio.NewScanner(stderr), EventStderr)

	wg.Wait()
	waitErr := cmd.Wait()

	if waitErr != nil {

		conflicts, detectErr := r.detectConflicts()
		if detectErr == nil && len(conflicts) > 0 {
			out <- ActionEvent{
				Type:  EventConflict,
				Files: conflicts,
			}
			return nil
		}

		out <- ActionEvent{
			Type: EventError,
			Data: waitErr.Error(),
		}

		return nil
	}

	out <- ActionEvent{
		Type: EventDone,
	}

	return nil
}
 
// builds an *exec.Cmd with the runner's repoPath as the working directory
// and GIT_TERMINAL_PROMPT=0 set to prevent hanging on credential prompts
func (r *Runner) gitCmd(ctx context.Context, args ...string) *exec.Cmd { 
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.repoPath
	cmd.Env = append(
		os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
	)

	return cmd
}
 
// check if repository is mid-operation
func (r *Runner) validateNotInProgress(allow string) error {
	gitDir := filepath.Join(r.repoPath, ".git")

	checks := []struct {
		Name string
		Path string
	}{
		{
			Name: "merge",
			Path: filepath.Join(gitDir, "MERGE_HEAD"),
		},
		{
			Name: "rebase",
			Path: filepath.Join(gitDir, "rebase-merge"),
		},
		{
			Name: "rebase",
			Path: filepath.Join(gitDir, "rebase-apply"),
		},
		{
			Name: "cherry-pick",
			Path: filepath.Join(gitDir, "CHERRY_PICK_HEAD"),
		},
		{
			Name: "revert",
			Path: filepath.Join(gitDir, "REVERT_HEAD"),
		},
	}

	for _, check := range checks {
		if check.Name == allow { continue }

		_, err := os.Stat(check.Path)

		if err == nil {
			return fmt.Errorf(
				"cannot start operation: %s already in progress",
				check.Name,
			)
		}

		if !errors.Is(err, os.ErrNotExist) { return err }
	}

	return nil
}