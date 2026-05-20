package git
 
import (
	"strings"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/aloceprng/keyboard-first-git-visualizer/backend/internal/graph"
)
 
// build bidirectional commit map (each commit knows both parents and children)
func CollectCommits(repo *git.Repository, tips []plumbing.Hash, refIndex map[string][]string)(map[plumbing.Hash]*graph.Commit, error) {
	commits := make(map[plumbing.Hash]*graph.Commit)
	seen := make(map[plumbing.Hash]bool)

	stack := make([]plumbing.Hash, 0, len(tips))
	stack = append(stack, tips...)

	for len(stack) > 0 {
		hash := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if seen[hash] { continue }
		seen[hash] = true

		obj, err := repo.CommitObject(hash)
		if err != nil {
			if err == plumbing.ErrObjectNotFound {
				tagObj, tagErr := repo.TagObject(hash)
				if tagErr != nil { continue }

				obj, err = repo.CommitObject(tagObj.Target)
				if err != nil { continue }

				seen[obj.Hash] = true
			} else { return nil, err }
		}

		commits[obj.Hash] = toGraphCommit(obj)

		for _, parentHash := range obj.ParentHashes {
			if !seen[parentHash] { stack = append(stack, parentHash) }
		}
	}

	buildChildIndex(commits)
	joinRefs(commits, refIndex)

	return commits, nil
}
 
// iterates commit map and populates ChildSHAs
func buildChildIndex(commits map[plumbing.Hash]*graph.Commit) {
	for _, commit := range commits {
		for _, parentSHA := range commit.ParentSHAs {

			parentHash := plumbing.NewHash(parentSHA)
			
			parent, ok := commits[parentHash]
			if !ok { continue}

			parent.ChildSHAs = append(parent.ChildSHAs, commit.SHA)
		}
	}
}

// attach ref names to each commit
func joinRefs(commits map[plumbing.Hash]*graph.Commit, refIndex map[string][]string) {
	for hash, commit := range commits {
		refs := refIndex[hash.String()]
		commit.Refs = append(commit.Refs, refs...)
	}
}
 
// toGraphCommit converts a go-git commit object into our internal Commit struct.
func toGraphCommit(c *object.Commit) *graph.Commit {
	subject := c.Message
	body := ""

	if idx := strings.Index(subject, "\n"); idx >= 0 {
		body = strings.TrimSpace(subject[idx+1:])
		subject = subject[:idx]
	}

	parentSHAs := make([]string, 0, len(c.ParentHashes))
	for _, p := range c.ParentHashes { parentSHAs = append(parentSHAs, p.String()) }

	return &graph.Commit{
		SHA: c.Hash.String(),
		ShortSHA: c.Hash.String()[:7],

		Author: graph.Identity{
			Name: c.Author.Name,
			Email: c.Author.Email,
		},

		Committer: graph.Identity{
			Name: c.Committer.Name,
			Email: c.Committer.Email,
		},

		Timestamp: c.Author.When,
		Subject: subject,
		Body: body,

		ParentSHAs: parentSHAs,
		ChildSHAs: make([]string, 0),
		Refs: make([]string, 0),

		Edges: make([]graph.Edge, 0),
	}
}