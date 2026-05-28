package action

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

func arg(args map[string]string, key, fallback string) string {
	if v, ok := args[key]; ok && v != "" {
		return v
	}
	return fallback
}
 
// switches HEAD to a branch or detaches it to a specific commit
// args: {"target": "main"} for a branch, {"target": "abc1234", "detach": "true"} for a SHA.
func (r *Runner) checkout(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	target := args["target"]
	if target == "" { return fmt.Errorf("checkout requires target") }

	detach := args["detach"] == "true"

	if detach {
		cmd := r.gitCmd(ctx, "checkout", "--detach", target)
		return r.streamSubprocess(ctx, cmd, out)
	}

	cmd := r.gitCmd(ctx, "checkout", target)
	return r.streamSubprocess(ctx, cmd, out)
}

// creates a new branch at the given start point
// args: {"name": "feature/x", "start": "main"} — start defaults to HEAD if omitted
// optionally checks out the new branch if "checkout": "true" is set.
func (r *Runner) branchCreate(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
    name  := args["name"]
    if name == "" { return fmt.Errorf("branchCreate requires name") }
    start := arg(args, "start", "HEAD")

    if args["checkout"] == "true" {
        return r.streamSubprocess(ctx, r.gitCmd(ctx, "checkout", "-b", name, start), out)
    }
    return r.streamSubprocess(ctx, r.gitCmd(ctx, "branch", name, start), out)
}

// deletes a branch, requires confirm=true in ActionRequest
// args: {"name": "feature/x", "force": "true"} — force maps to -D vs -d.
func (r *Runner) branchDelete(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	name := args["name"]
	if name == "" { return fmt.Errorf("branchDelete requires name") }

	force := args["force"] == "true"

	flag := "-d"
	if force { flag = "-D" }

	cmd := r.gitCmd(ctx, "branch", flag, name)
	return r.streamSubprocess(ctx, cmd, out)
}
 
// branchRename renames a local branch.
// args: {"from": "old-name", "to": "new-name"}.
func (r *Runner) branchRename(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	from := args["from"]
	to := args["to"]

	if from == "" || to == "" {	return fmt.Errorf("branchRename requires from and to") }

	cmd := r.gitCmd(ctx, "branch", "-m", from, to)
	return r.streamSubprocess(ctx, cmd, out)
}
 
// merges the given branch into the current branch.
// args: {"branch": "feature/x", "strategy": "merge"|"squash"|"ff-only"}.
func (r *Runner) merge(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	branch := args["branch"]
	if branch == "" { return fmt.Errorf("merge requires branch") }

	strategy := args["strategy"]

	switch strategy {
	case "squash":
		return r.streamSubprocess(ctx, r.gitCmd(ctx, "merge", "--squash", branch), out)

	case "ff-only":
		return r.streamSubprocess(ctx, r.gitCmd(ctx, "merge", "--ff-only", branch), out)

	default:
		return r.streamSubprocess(ctx, r.gitCmd(ctx, "merge", branch), out)
	}
}
 
// rebase rebases the current branch (or a specified branch) onto a target
// args: {"onto": "main", "branch": "feature/x"} — branch defaults to HEAD
func (r *Runner) rebase(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	onto := args["onto"]
	if onto == "" { return fmt.Errorf("rebase requires onto") }

	branch := args["branch"]

	if branch != "" && branch != "HEAD" {
		return r.streamSubprocess(
			ctx,
			r.gitCmd(ctx, "rebase", onto, branch),
			out,
		)
	}

	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "rebase", onto),
		out,
	)
}
 
// aborts an in-progress rebase, restoring the pre-rebase state
func (r *Runner) rebaseAbort(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "rebase", "--abort"),
		out,
	)
}
 
// resumes a paused rebase after the user has resolved conflicts
func (r *Runner) rebaseContinue(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "rebase", "--continue"),
		out,
	)
}
 
// creates a new commit that undoes the changes introduced by a given SHA
// args: {"sha": "abc1234", "no_commit": "true"} — no_commit stages the revert
func (r *Runner) revert(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	sha := args["sha"]
	if sha == "" { return fmt.Errorf("revert requires sha") }

	noCommit := args["no_commit"] == "true"

	if noCommit {
		return r.streamSubprocess(
			ctx,
			r.gitCmd(ctx, "revert", "--no-commit", sha),
			out,
		)
	}

	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "revert", sha),
		out,
	)
}
 
// applies the changes from a given commit onto the current branch
// args: {"sha": "abc1234", "no_commit": "true"}
func (r *Runner) cherryPick(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	sha := args["sha"]
	if sha == "" { return fmt.Errorf("cherryPick requires sha") }

	noCommit := args["no_commit"] == "true"

	if noCommit {
		return r.streamSubprocess(
			ctx,
			r.gitCmd(ctx, "cherry-pick", "--no-commit", sha),
			out,
		)
	}

	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "cherry-pick", sha),
		out,
	)
}
 
// abort in-progress cherry-pick
func (r *Runner) cherryPickAbort(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "cherry-pick", "--abort"),
		out,
	)
}
 
// resetSoft moves HEAD (and the current branch pointer) to the given SHA,
// leaving staged changes and the working tree untouched
// args: {"sha": "abc1234"} or {"steps": "2"} (steps back from HEAD)
func (r *Runner) resetSoft(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	sha := args["sha"]
	steps := args["steps"]

	target := sha
	if target == "" && steps != "" { target = fmt.Sprintf("HEAD~%s", steps) }
	if target == "" { return fmt.Errorf("resetSoft requires sha or steps") }

	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "reset", "--soft", target),
		out,
	)
}
 
// moves HEAD to the given SHA and discards all staged and unstaged changes
// args: {"sha": "abc1234"} or {"steps": "1"}
func (r *Runner) resetHard(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	sha := args["sha"]
	steps := args["steps"]

	target := sha
	if target == "" && steps != "" { target = fmt.Sprintf("HEAD~%s", steps) }
	if target == "" { return fmt.Errorf("resetHard requires sha or steps") }

	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "reset", "--hard", target),
		out,
	)
}
 
// moves HEAD to the given SHA and unstages changes, but leaves the working tree intact
// args: {"sha": "abc1234"} or {"steps": "1"}
func (r *Runner) resetMixed(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	sha := args["sha"]
	steps := args["steps"]

	target := sha
	if target == "" && steps != "" { target = fmt.Sprintf("HEAD~%s", steps) }
	if target == "" { return fmt.Errorf("resetMixed requires sha or steps") }

	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "reset", "--mixed", target),
		out,
	)
}
 
// saves the current dirty working tree and index to the stash stack, restoring a clean working tree
// args: {"message": "wip: optional label"}
func (r *Runner) stash(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	msg := args["message"]

	if msg != "" {
		return r.streamSubprocess(
			ctx,
			r.gitCmd(ctx, "stash", "push", "-m", msg),
			out,
		)
	}

	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "stash"),
		out,
	)
}
 
// applies the top stash entry to the working tree and drops it from the stash stack
// args: {"index": "0"} — index into the stash list, defaults to 0 (most recent)
func (r *Runner) stashPop(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	index := args["index"]

	if index == "" || index == "0" {
		return r.streamSubprocess(
			ctx,
			r.gitCmd(ctx, "stash", "pop"),
			out,
		)
	}

	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "stash", "pop", fmt.Sprintf("stash@{%s}", index)),
		out,
	)
}
 
// removes a stash entry without applying it
// args: {"index": "0"}
func (r *Runner) stashDrop(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	index := args["index"]
	if index == "" { index = "0" }

	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "stash", "drop", fmt.Sprintf("stash@{%s}", index)),
		out,
	)
}

// creates a lightweight or annotated tag at the given commit
// args: {"name": "v1.2.3", "sha": "abc1234", "message": "optional"}
func (r *Runner) tag(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	name := args["name"]
	sha := args["sha"]
	msg := args["message"]

	if name == "" { return fmt.Errorf("tag requires name") }
	if sha == "" { sha = "HEAD" }
	if msg != "" {
		return r.streamSubprocess(
			ctx,
			r.gitCmd(ctx, "tag", "-a", name, sha, "-m", msg),
			out,
		)
	}

	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "tag", name, sha),
		out,
	)
}
 
// deletes a local tag
// args: {"name": "v1.2.3"}
func (r *Runner) tagDelete(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	name := args["name"]
	if name == "" { return fmt.Errorf("tagDelete requires name") }

	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, "tag", "-d", name),
		out,
	)
}
 
// fetches from the default or named remote without merging
// args: {"remote": "origin"} — remote defaults to "origin"
func (r *Runner) fetch(ctx context.Context, args map[string]string, out chan<- ActionEvent) error {
	remote := arg(args, "remote", "origin")

	cmdArgs := []string{"fetch", remote}

	if args["prune"] == "true" { cmdArgs = append(cmdArgs, "--prune") }

	return r.streamSubprocess(
		ctx,
		r.gitCmd(ctx, cmdArgs...),
		out,
	)
}
 
// expands a partial SHA or ref name to a full 40-char SHA
func (r *Runner) resolveSHA(ref string) (string, error) {
	if ref == "" { return "", fmt.Errorf("empty ref") }

	cmd := r.gitCmd(context.Background(), "rev-parse", "--verify", ref)

	out, err := cmd.Output()
	if err != nil {	return "", fmt.Errorf("failed to resolve ref %q: %w", ref, err) }

	sha := strings.TrimSpace(string(out))
	if sha == "" { return "", fmt.Errorf("empty SHA resolved for %q", ref) }

	return sha, nil
}
 
// converts a "steps" argument ("2") into the equivalent SHA by walking HEAD back N commits via the reflog
func (r *Runner) resolveSteps(steps string) (string, error) { 
	if steps == "" { return "", fmt.Errorf("empty steps") }

	n, err := strconv.Atoi(steps)
	if err != nil {	return "", fmt.Errorf("invalid steps %q: must be integer", steps) }
	if n < 0 { return "", fmt.Errorf("steps must be >= 0") }

	ref := "HEAD"
	if n > 0 { ref = fmt.Sprintf("HEAD~%d", n) }

	return r.resolveSHA(ref)
}