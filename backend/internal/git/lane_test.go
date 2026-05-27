package git

import (
    "testing"
    "time"
    "github.com/aloceprng/keyboard-first-git-visualizer/backend/internal/graph"
)

// builds a graph.Commit with only the fields the lane algorithm uses
func makeCommit(sha string, parentSHAs ...string) *graph.Commit {
    short := sha
    if len(short) > 4 { short = short[:4] }

    return &graph.Commit{
        SHA:        sha,
        ShortSHA:   short,
        Timestamp:  time.Now(),
        ParentSHAs: parentSHAs,
        ChildSHAs:  make([]string, 0),
        Edges:      make([]graph.Edge, 0),
    }
}

// wire up ChildSHAs and returns commits in the order you pass them
func buildSorted(commits ...*graph.Commit) []*graph.Commit {
    index := make(map[string]*graph.Commit)
    for _, c := range commits { index[c.SHA] = c }

    for _, c := range commits {
        for _, pSHA := range c.ParentSHAs {
            if p, ok := index[pSHA]; ok {
                p.ChildSHAs = append(p.ChildSHAs, c.SHA)
            }
        }
    }
    return commits
}

//tests

// linear history
func TestAssignLanes_Linear(t *testing.T) {
    c3 := makeCommit("c3", "c2")
    c2 := makeCommit("c2", "c1")
    c1 := makeCommit("c1")

    rows, err := AssignLanes(buildSorted(c3, c2, c1))
    if err != nil { t.Fatal(err) }

    for i, row := range rows {
        if row.Commit.Lane != 0 {
            t.Errorf("row %d: want lane 0, got %d", i, row.Commit.Lane)
        }
    }
}

// diverging branches
func TestAssignLanes_TwoBranches(t *testing.T) {
    base := makeCommit("base")
    a    := makeCommit("a", "base")
    b    := makeCommit("b", "base")

    rows, _ := AssignLanes(buildSorted(a, b, base))

    aLane    := rows[0].Commit.Lane
    bLane    := rows[1].Commit.Lane
    baseLane := rows[2].Commit.Lane

    if aLane == bLane {
        t.Errorf("A and B should be in different lanes, both got %d", aLane)
    }

    if baseLane != aLane && baseLane != bLane {
        t.Errorf("base lane %d not in either branch lane (%d, %d)", baseLane, aLane, bLane)
    }
}

// merging branches:
//   M 
//   |\
//   |  F
//   |  |
//   P  |
//   | /
//   base
func TestAssignLanes_MergeProducesEdge(t *testing.T) {
    base        := makeCommit("base")
    mainPrev    := makeCommit("mainPrev", "base")
    featureTip  := makeCommit("featureTip", "base")
    merge       := makeCommit("merge", "mainPrev", "featureTip")

    rows, _ := AssignLanes(buildSorted(merge, featureTip, mainPrev, base))

    mergeRow := rows[0]
    hasMergeEdge := false
    for _, e := range mergeRow.Commit.Edges {
        if e.Type == graph.EdgeMergeIn || e.Type == graph.EdgeBranchOut {
            hasMergeEdge = true
        }
    }
    if !hasMergeEdge {
        t.Error("merge commit should have at least one non-straight edge")
    }
}

// stability threshold testing
func TestAssignLanes_StabilityThreshold(t *testing.T) {
    base  := makeCommit("base")
    a1    := makeCommit("a1", "base")
    a2    := makeCommit("a2", "a1")
    merge := makeCommit("merge", "base", "a2")

    rows, _ := AssignLanes(buildSorted(merge, a2, a1, base))

    laneA := rows[1].Commit.Lane
    for i := 2; i < len(rows) && i < 2+stabilityThreshold; i++ {
        if rows[i].Commit.Lane == laneA && rows[i].Commit.SHA != "a1" {
            t.Errorf("lane %d reused too early at row %d (threshold not respected)", laneA, i)
        }
    }
}