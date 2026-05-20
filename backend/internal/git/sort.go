package git
 
import (
	"fmt"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/aloceprng/keyboard-first-git-visualizer/backend/internal/graph"
)
 
// implements Kahn's algorithm over the commit map, where every child appears before all of its parents
func TopologicalSort(commits map[plumbing.Hash]*graph.Commit) ([]*graph.Commit, error) {
	childCounts := computeChildCounts(commits)
	queue := initialQueue(commits, childCounts)
	sorted := make([]*graph.Commit, 0, len(commits))

	for len(queue) > 0 {
		var current *graph.Commit
		current, queue = popHighestTimestamp(queue)
		sorted = append(sorted, current)

		for _, parentSHA := range current.ParentSHAs {
			childCounts[parentSHA]--

			if childCounts[parentSHA] == 0 {
				parentHash := plumbing.NewHash(parentSHA)
				parent, exists := commits[parentHash]

				if exists {	queue = append(queue, parent) }
			}
		}
	}

	if len(sorted) != len(commits) {
		return nil, fmt.Errorf("cycle detected in commit graph: sorted %d of %d commits", len(sorted), len(commits))
	}

	return sorted, nil
}
 
// Kahn's indegree: returns a map of SHA → number of children not yet emitted
func computeChildCounts(commits map[plumbing.Hash]*graph.Commit) map[string]int {
	count := make(map[string]int)

	for _, commit := range commits{ count[commit.SHA] = len(commit.ChildSHAs) }

	return count
}
 
// returns all commits with no children at init
func initialQueue(commits map[plumbing.Hash]*graph.Commit, childCounts map[string]int) []*graph.Commit {
	queue := make([]*graph.Commit, 0)

	for _, commit := range commits {
		if childCounts[commit.SHA] == 0 { queue = append(queue, commit) }
	}

	return queue
}
 
// removes and returns the commit with the most recent timestamp from the ready queue
func popHighestTimestamp(queue []*graph.Commit) (*graph.Commit, []*graph.Commit) {
	if len(queue) == 0 { return nil, queue }

	bestIndex := 0
	bestTime := queue[0].Timestamp

	for i := 1; i < len(queue); i++ {
		if queue[i].Timestamp.After(bestTime) {
			bestIndex = i
			bestTime = queue[i].Timestamp
		}
	}

	best := queue[bestIndex]
	
	copy(queue[bestIndex:], queue[bestIndex+1:])
	queue[len(queue)-1] = nil
	queue = queue[:len(queue)-1]
	
	return best, queue
}

