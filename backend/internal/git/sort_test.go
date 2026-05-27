package git

import (
    "fmt"
    "testing"
	"time"
    "github.com/go-git/go-git/v5/plumbing"
    "github.com/aloceprng/keyboard-first-git-visualizer/backend/internal/graph"
)

func buildTestRepo(count int) map[plumbing.Hash]*graph.Commit {
    commits := make([]*graph.Commit, count)
    now := time.Now()

    for i := 0; i < count; i++ {
        sha := fmt.Sprintf("c%03d", i)
        parentSHAs := make([]string, 0)
        if i > 0 {
            parentSHAs = []string{fmt.Sprintf("c%03d", i-1)}
        }

        commits[i] = &graph.Commit{
            SHA:        sha,
            ShortSHA:   sha[:4],
            Timestamp:  now.Add(time.Duration(i) * time.Minute),
            ParentSHAs: parentSHAs,
            ChildSHAs:  make([]string, 0),
            Edges:      make([]graph.Edge, 0),
        }
    }

    index := make(map[string]*graph.Commit)
    for _, c := range commits {
        index[c.SHA] = c
    }
    for _, c := range commits {
        for _, parentSHA := range c.ParentSHAs {
            if parent, ok := index[parentSHA]; ok {
                parent.ChildSHAs = append(parent.ChildSHAs, c.SHA)
            }
        }
    }

    result := make(map[plumbing.Hash]*graph.Commit)
    for _, c := range commits {
        result[plumbing.NewHash(c.SHA)] = c
    }

    return result
}

func TestTopologicalSort_ChildBeforeParent(t *testing.T) {
    commits := buildTestRepo(50)
    sorted, err := TopologicalSort(commits)
    if err != nil { t.Fatal(err) }

    position := make(map[string]int)
    for i, c := range sorted { position[c.SHA] = i }

    for _, c := range sorted {
        for _, parentSHA := range c.ParentSHAs {
            childPos  := position[c.SHA]
            parentPos := position[parentSHA]
            if childPos > parentPos {
                t.Errorf("commit %s (pos %d) appears after parent %s (pos %d)",
                    c.SHA[:4], childPos, parentSHA[:4], parentPos)
            }
        }
    }
}

// cycle
func TestTopologicalSort_CycleDetected(t *testing.T) {
    a := makeCommit("aaaa", "bbbb")
    b := makeCommit("bbbb", "aaaa")
    a.ChildSHAs = []string{"bbbb"}
    b.ChildSHAs = []string{"aaaa"}

    commits := map[plumbing.Hash]*graph.Commit{
        plumbing.NewHash("aaaa"): a,
        plumbing.NewHash("bbbb"): b,
    }

    _, err := TopologicalSort(commits)
    if err == nil { t.Error("expected cycle detection error, got nil") }
}

// two independent tips
func TestTopologicalSort_TimestampOrdering(t *testing.T) {
    now   := time.Now()
    older := makeCommit("older")
    older.Timestamp = now.Add(-1 * time.Hour)
    newer := makeCommit("newer")
    newer.Timestamp = now

    commits := map[plumbing.Hash]*graph.Commit{
        plumbing.NewHash("older"): older,
        plumbing.NewHash("newer"): newer,
    }

    sorted, _ := TopologicalSort(commits)
    if sorted[0].SHA != "newer" {
        t.Errorf("want newer first, got %s first", sorted[0].SHA[:4])
    }
}
