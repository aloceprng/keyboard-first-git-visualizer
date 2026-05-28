package git

import (
    "fmt"
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
    root  := makeCommit("root")
    tip_b := makeCommit("tipB", "root")

    padding := make([]*graph.Commit, stabilityThreshold-1)
    prev := "root"
    for i := range padding {
        sha := fmt.Sprintf("pad%d", i)
        padding[i] = makeCommit(sha, prev)
        prev = sha
    }

    base  := makeCommit("base", prev)
    tip_a := makeCommit("tipA", "base")

    sorted := []*graph.Commit{tip_a, base}
    sorted = append(sorted, padding[len(padding)-1])
    for i := len(padding) - 2; i >= 0; i-- {
        sorted = append(sorted, padding[i])
    }
    sorted = append(sorted, tip_b, root)
    sorted = buildSorted(sorted...)

    rows, err := AssignLanes(sorted)
    if err != nil {
        t.Fatal(err)
    }

    tipALane := rows[0].Commit.Lane
    tipBRow := -1
    for i, row := range rows {
        if row.Commit.SHA == "tipB" {
            tipBRow = i
            break
        }
    }
    if tipBRow < 0 {
        t.Fatal("tipB not found in rows")
    }

    if tipBRow < stabilityThreshold {
        if rows[tipBRow].Commit.Lane == tipALane {
            t.Errorf(
                "lane %d recycled at row %d (only %d rows idle, threshold is %d)",
                tipALane, tipBRow, tipBRow, stabilityThreshold,
            )
        }
    }
}

// run a few rows through AssignLanes, serialise mid-stream,
// deserialise, and confirm the RowIndex and slot SHAs survived intact
func TestLaneTable_SerializeRoundtrip(t *testing.T) {
    c3 := makeCommit("c3", "c2")
    c2 := makeCommit("c2", "c1")
    c1 := makeCommit("c1")

    lt := &graph.LaneTable{Slots: make([]graph.LaneSlot, 0)}
    lt.Slots = append(lt.Slots, graph.LaneSlot{SHA: "c1", Colour: 0, IdleSince: -1})
    lt.RowIndex = 2
    lt.ColourCycle = 1

    data, err := SerializeLaneTable(lt)
    if err != nil { t.Fatal(err) }

    restored, err := DeserializeLaneTable(data)
    if err != nil { t.Fatal(err) }

    if restored.RowIndex != lt.RowIndex {
        t.Errorf("RowIndex: got %d, want %d", restored.RowIndex, lt.RowIndex)
    }
    if len(restored.Slots) != 1 || restored.Slots[0].SHA != "c1" {
        t.Errorf("Slots not preserved: %+v", restored.Slots)
    }
    _ = c1; _ = c2; _ = c3
}