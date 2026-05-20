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
		lane := lt.findOrClaimLane(commit.SHA)
		
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
					lt.handleConvergence(lane, parentLane),
				)

				lt.freeLane(lane)
			} else {
				lt.continueLane(lane, firstParent)

				edges = append(edges, graph.Edge{
					FromLane: lane,
					ToLane:   lane,
					ToRow:    lt.RowIndex + 1,
					Type:     graph.EdgeStraight,
				})
			}

		} else { lt.freeLane(lane) }

		mergeEdges := lt.handleMergeParents(commit, lane)
		edges = append(edges, mergeEdges...)

		commit.Edges = edges
		commit.Passthrough = lt.buildPassthrough(
			lane,
			edges,
		)

		rows = append(rows, &graph.Row{
			Commit:      commit,
			ActiveLanes: lt.maxActiveLane(),
		})

		lt.RowIndex++
	}

	return rows, nil
}
 
// looks for the commit's SHA in the lane table and returns its index
// if not found, allocates or reuses a lane for a new branch
func (lt *graph.LaneTable) findOrClaimLane(sha string) int { 
	for i, slot := range lt.Slots{
		if slot.SHA == sha{
			return i
		}
	}

	return lt.openNewLane(sha)
}
 
// replace the current SHA in a slot with the first parent's SHA, keeping the lane alive for the next commit
func (lt *graph.LaneTable) continueLane(lane int, parentSHA string) {
	lt.Slots[lane].SHA = parentSHA
	lt.Slots[lane].IdleSince = -1
}
 
// appends a new slot or reuses an idle one, and assigns the next colour from the cycle
func (lt *graph.LaneTable) openNewLane(parentSHA string) int { 

	reusable := lt.enforceStability()

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
func (lt *graph.LaneTable) freeLane(lane int) {
	lt.Slots[lane].SHA = ""
	lt.Slots[lane].IdleSince = lt.RowIndex
}
 
// returns the lowest-indexed slot that has been idle for at
// least stabilityThreshold rows, or -1 if none qualify.
const stabilityThreshold = 8
 
func (lt *graph.LaneTable) enforceStability() int { 
	for i, slot := range lt.Slots {
		if slot.SHA != "" { continue }
		if slot.IdleSince < 0 { continue }
		if lt.RowIndex-slot.IdleSince >= stabilityThreshold { return i }
	}

	return -1
}
 
// computes the bitmask of lanes that are active at this row
func (lt *graph.LaneTable) buildPassthrough(commitLane int, edges []graph.Edge) uint64 {
	var mask uint64

	involved := make(map[int]bool)
	involved[commitLane] = true

	for _, edge := range edges {
		involved[edge.FromLane] = true
		involved[edge.ToLane] = true
	}

	for i, slot := range lt.Slots {
		if slot.SHA == "" { continue }
		if involved[i] { continue }

		if i < 64 {	mask |= uint64(1) << i }
	}

	return mask
}
 
// processes every parent beyond the first on a merge commit
// for each: if the parent already has a slot, emit a converging EdgeMergeIn
// otherwise, open a new slot and emit a diverging EdgeBranchOut
func (lt *graph.LaneTable) handleMergeParents(commit *graph.Commit, commitLane int) []graph.Edge {
	
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

		newLane := lt.openNewLane(parentSHA)

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
func (lt *graph.LaneTable) handleConvergence(commitLane int, parentLane int) graph.Edge {
	return graph.Edge{
		FromLane: commitLane,
		ToLane: parentLane,
		ToRow: lt.RowIndex + 1,
		Type: graph.EdgeMergeIn,
	}
}
 
// encodes the lane table to JSON for use as a pagination cursor
// frontend sends this opaque blob back with the next page request
func (lt *graph.LaneTable) Serialize() ([]byte, error) { 
	return json.Marshal(lt)
}
 
// decodes a pagination cursor back into a usable LaneTable
func DeserializeLaneTable(data []byte) (*graph.LaneTable, error) { 
	var lt graph.LaneTable

	if err := json.Unmarshal(data, &lt); err != nil { return nil, err }

	return &lt, nil	
}
 
// returns the highest occupied lane index + 1
func (lt *graph.LaneTable) maxActiveLane() int { 
    highest := 0
    for i, slot := range lt.Slots {
        if slot.SHA != "" { highest = i + 1 }
    }

    return highest
}
 
