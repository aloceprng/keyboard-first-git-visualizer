package git
 
import (
	"fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)
 
// opening git repo
func OpenRepo(startPath string) (*git.Repository, error) { 
	r, err := git.PlainOpenWithOptions(startPath, &git.PlainOpenOptions{DetectDotGit: true})

	if err != nil{
		fmt.Printf("error opening repository %s", err)
		return nil, err
	}

	return r, err 
}
 
// walk up from startPath until .git directory
// note! go-git's PlainOpenOptions already finds root; here, the repository's location on disk returned, making direct .git access easier
func FindRepoRoot(startPath string) (string, error) { 
	path, err := filepath.Abs(startPath)
	if err != nil { return "", err }

	for {
		gitPath := filepath.Join(path, ".git")

		_, err := os.Stat(gitPath)
		if err == nil { return path, nil }

		parent := filepath.Dir(path)
		if parent == path { break }

		path = parent
	}

	return "", fmt.Errorf("no git repository found from %s", startPath)
}
 
// returns the tip SHA of every ref in the repo, deduplicating if necessary. this is the base set for collect
func WalkAllRefs(repo *git.Repository) ([]plumbing.Hash, error) { 
	refs, err := repo.References()
	if err != nil { return nil, fmt.Errorf("can't find refs in repo: %w", err) }

	var hashes []plumbing.Hash
	seen := make(map[plumbing.Hash]bool)

	for {
		ref, err := refs.Next()
		if err == io.EOF { break }
		if err != nil { return nil, err }
		if ref.Type() == plumbing.SymbolicReference { continue }

		hash := ref.Hash() // this gets the tip SHA 
		if !seen[hash] {
			seen[hash] = true
			hashes = append(hashes, hash)
		}
	}
	
	return hashes, nil 
}
 
// returns ref→SHA and SHA→[]ref maps
func BuildRefIndex(repo *git.Repository) (map[string]string, map[string][]string, error) {
	refs, err := repo.References()
	if err != nil { return nil, nil, fmt.Errorf("failed to read refs: %w", err)	}

	refToSHA := make(map[string]string)
	shaToRef := make(map[string][]string)

	for {
		ref, err := refs.Next()
		if err == io.EOF { break }
		if err != nil {	return nil, nil, err }
		if ref.Type() == plumbing.SymbolicReference { continue }

		refName := ref.Name().String()
		hash := ref.Hash().String()

		refToSHA[refName] = hash
		shaToRef[hash] = append(shaToRef[hash], refName)
	}

	return refToSHA, shaToRef, nil
}
 
// checks for the presence of MERGE_HEAD, rebase-merge/, rebase-apply/, CHERRY_PICK_HEAD, REVERT_HEAD, BISECT_LOG
func InProgressState(repoPath string) (string, error) {
	gitDir := filepath.Join(repoPath, ".git")

	checks := []struct {
		path  string
		state string
	}{
		{"MERGE_HEAD", "merge"},
		{"rebase-merge", "rebase"},
		{"rebase-apply", "rebase"},
		{"CHERRY_PICK_HEAD", "cherry-pick"},
		{"REVERT_HEAD", "revert"},
		{"BISECT_LOG", "bisect"},
	}

	for _, check := range checks {
		fullPath := filepath.Join(gitDir, check.path)

		_, err := os.Stat(fullPath)
		if err == nil {	return check.state, nil	}

		if !os.IsNotExist(err) { return "", err }
	}

	return "", nil
}
 
// additional metadata for the current in-progress operation
func InProgressMeta(repoPath string) (map[string]string, error) {
    gitDir := filepath.Join(repoPath, ".git")
    meta := make(map[string]string)

    rebaseMergeDir := filepath.Join(gitDir, "rebase-merge")
    if _, err := os.Stat(rebaseMergeDir); err == nil {
        if v, err := readTrimmedFile(filepath.Join(rebaseMergeDir, "msgnum")); err == nil {
            meta["step"] = v
        }
        if v, err := readTrimmedFile(filepath.Join(rebaseMergeDir, "end")); err == nil {
            meta["total"] = v
        }
        if v, err := readTrimmedFile(filepath.Join(rebaseMergeDir, "onto")); err == nil {
            meta["onto"] = v
        }
        if v, err := readTrimmedFile(filepath.Join(rebaseMergeDir, "head-name")); err == nil {
            meta["branch"] = v
        }
        return meta, nil
    }

    rebaseApplyDir := filepath.Join(gitDir, "rebase-apply")
    if _, err := os.Stat(rebaseApplyDir); err == nil {
        if v, err := readTrimmedFile(filepath.Join(rebaseApplyDir, "next")); err == nil {
            meta["step"] = v
        }
        if v, err := readTrimmedFile(filepath.Join(rebaseApplyDir, "last")); err == nil {
            meta["total"] = v
        }
        if v, err := readTrimmedFile(filepath.Join(rebaseApplyDir, "onto")); err == nil {
            meta["onto"] = v
        }
        return meta, nil
    }

    if sha, err := readTrimmedFile(filepath.Join(gitDir, "CHERRY_PICK_HEAD")); err == nil {
        meta["sha"] = sha
        return meta, nil
    }

    if sha, err := readTrimmedFile(filepath.Join(gitDir, "MERGE_HEAD")); err == nil {
        meta["sha"] = sha
        if msg, err := readTrimmedFile(filepath.Join(gitDir, "MERGE_MSG")); err == nil {
            meta["message"] = msg
        }
        return meta, nil
    }

    if sha, err := readTrimmedFile(filepath.Join(gitDir, "REVERT_HEAD")); err == nil {
        meta["sha"] = sha
        return meta, nil
    }

    return meta, nil
}

func readTrimmedFile(path string) (string, error) {
    data, err := os.ReadFile(path)
    if err != nil { return "", err }

    return strings.TrimSpace(string(data)), nil
}