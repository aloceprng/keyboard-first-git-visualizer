package git
 
import (
	"encoding/json"
	"github.com/aloceprng/keyboard-first-git-visualizer/backend/internal/graph"
)
 
// walks the topologically sorted commit list, maintains the lane table, and writes lane info onto commit
func AssignLanes(sorted []*graph.Commit) ([]*graph.Row, error) { 
	lt := &graph.LaneTable{
        Slots: make([]graph.LaneSlot, 0),
        ColourCycle: 0,
        RowIndex:    0,
    }
    rows := make([]*graph.Row, 0, len(sorted))

	for _, commit := range sorted{
		preActive := make([]bool, len(lt.Slots))
		for i, slot := range lt.Slots {
			preActive[i] = slot.SHA != ""
		}

		lane := findOrClaimLane(lt, commit.SHA)

		commit.Lane = lane
		commit.Row = lt.RowIndex

		edges := make([]graph.Edge, 0)

		if len(commit.ParentSHAs) > 0 {

			firstParent := commit.ParentSHAs[0]
			parentLane := -1

			for i, slot := range lt.Slots {
				if i == lane { continue }

				if slot.SHA == firstParent {
					parentLane = i
					break
				}
			}

			if parentLane >= 0 {
				edges = append(edges,
					handleConvergence(lt, lane, parentLane),
				)

				freeLane(lt, lane)
			} else {
				continueLane(lt, lane, firstParent)

				edges = append(edges, graph.Edge{
					FromLane: lane,
					ToLane:   lane,
					ToRow:    lt.RowIndex + 1,
					Type:     graph.EdgeStraight,
				})
			}

		} else { freeLane(lt, lane) }

		mergeEdges := handleMergeParents(lt, commit, lane)
		edges = append(edges, mergeEdges...)

		commit.Edges = edges
		commit.Passthrough = buildPassthrough(lt, lane, preActive)

		rows = append(rows, &graph.Row{
			Commit:      commit,
			ActiveLanes: rowActiveLanes(lt, lane, edges),
		})

		lt.RowIndex++
	}

	return rows, nil
}
 
// looks for the commit's SHA in the lane table and returns its index
// if not found, allocates or reuses a lane for a new branch
func findOrClaimLane(lt *graph.LaneTable, sha string) int { 
	for i, slot := range lt.Slots{
		if slot.SHA == sha{
			return i
		}
	}

	return openNewLane(lt, sha)
}
 
// replace the current SHA in a slot with the first parent's SHA, keeping the lane alive for the next commit
func continueLane(lt *graph.LaneTable, lane int, parentSHA string) {
	lt.Slots[lane].SHA = parentSHA
	lt.Slots[lane].IdleSince = -1
}
 
// appends a new slot or reuses an idle one, and assigns the next colour from the cycle
func openNewLane(lt *graph.LaneTable, parentSHA string) int { 

	reusable := enforceStability(lt)

	if reusable >= 0 {
		lt.Slots[reusable].SHA = parentSHA
		lt.Slots[reusable].IdleSince = -1
		lt.Slots[reusable].Colour = lt.ColourCycle
		lt.ColourCycle++

		return reusable
	}

	lane := len(lt.Slots)

	lt.Slots = append(lt.Slots, graph.LaneSlot{
		SHA:       parentSHA,
		Colour:     lt.ColourCycle,
		IdleSince: -1,
	})

	lt.ColourCycle++

	return lane
}
 
// marks a slot as idle at the current row
func freeLane(lt *graph.LaneTable, lane int) {
	lt.Slots[lane].SHA = ""
	lt.Slots[lane].IdleSince = lt.RowIndex
}
 
// returns the lowest-indexed slot that has been idle for at
// least stabilityThreshold rows, or -1 if none qualify.
const stabilityThreshold = 8
 
func enforceStability(lt *graph.LaneTable) int { 
	for i, slot := range lt.Slots {
		if slot.SHA != "" { continue }
		if slot.IdleSince < 0 { continue }
		if lt.RowIndex-slot.IdleSince >= stabilityThreshold { return i }
	}

	return -1
}
 
// computes the bitmask of lanes drawn as a straight vertical bar through this
// row: lanes that were active coming into the row, are still active leaving it,
// and aren't the commit's own lane. Requiring pre-row activity excludes lanes
// opened on this row (branch-out targets, the tip's own lane), whose line
// starts here rather than passing through.
func buildPassthrough(lt *graph.LaneTable, commitLane int, preActive []bool) uint64 {
	var mask uint64

	for i, slot := range lt.Slots {
		if i == commitLane { continue }
		if slot.SHA == "" { continue }                  // freed — not active after this row
		if i >= len(preActive) || !preActive[i] { continue } // opened on this row
		if i < 64 { mask |= uint64(1) << i }
	}

	return mask
}

// number of lanes occupied at this row: the highest of any still-active lane,
// the commit's own lane, and any lane an edge touches
func rowActiveLanes(lt *graph.LaneTable, commitLane int, edges []graph.Edge) int {
	active := maxActiveLane(lt)
	if commitLane+1 > active { active = commitLane + 1 }

	for _, e := range edges {
		if e.FromLane+1 > active { active = e.FromLane + 1 }
		if e.ToLane+1 > active { active = e.ToLane + 1 }
	}

	return active
}
 
// processes every parent beyond the first on a merge commit
// for each: if the parent already has a slot, emit a converging EdgeMergeIn
// otherwise, open a new slot and emit a diverging EdgeBranchOut
func handleMergeParents(lt *graph.LaneTable, commit *graph.Commit, commitLane int) []graph.Edge {
	
	edges := make([]graph.Edge, 0)

	if len(commit.ParentSHAs) <= 1 { return edges }

	for _, parentSHA := range commit.ParentSHAs[1:] {
		parentLane := -1

		for i, slot := range lt.Slots {
			if slot.SHA == parentSHA {
				parentLane = i
				break
			}
		}

		if parentLane >= 0 {
			edges = append(edges, graph.Edge{
				FromLane: commitLane,
				ToLane: parentLane,
				ToRow: lt.RowIndex + 1,
				Type: graph.EdgeMergeIn,
			})
			continue
		}

		newLane := openNewLane(lt, parentSHA)

		edges = append(edges, graph.Edge{
			FromLane: commitLane,
			ToLane: newLane,
			ToRow: commit.Row + 1,
			Type: graph.EdgeBranchOut,
		})
	}

	return edges
}
 
// handles the case where the commit's first parent is already owned by a different active lane
func handleConvergence(lt *graph.LaneTable, commitLane int, parentLane int) graph.Edge {
	return graph.Edge{
		FromLane: commitLane,
		ToLane: parentLane,
		ToRow: lt.RowIndex + 1,
		Type: graph.EdgeMergeIn,
	}
}
 
// encodes the lane table to JSON for use as a pagination cursor
// frontend sends this opaque blob back with the next page request
func SerializeLaneTable(lt *graph.LaneTable) ([]byte, error) { 
	return json.Marshal(lt)
}
 
// decodes a pagination cursor back into a usable LaneTable
func DeserializeLaneTable(data []byte) (*graph.LaneTable, error) { 
	var lt graph.LaneTable

	if err := json.Unmarshal(data, &lt); err != nil { return nil, err }

	return &lt, nil	
}
 
// returns the highest occupied lane index + 1
func maxActiveLane(lt *graph.LaneTable) int { 
    highest := 0
    for i, slot := range lt.Slots {
        if slot.SHA != "" { highest = i + 1 }
    }

    return highest
}
 
