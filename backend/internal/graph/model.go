package graph
 
import "time"
 
type EdgeType int
 
const (
	EdgeStraight  EdgeType = iota
	EdgeMergeIn
	EdgeBranchOut   
	EdgePassthrough
)
 
type Identity struct {
	Name  string
	Email string
}
 
type Edge struct {
	FromLane int
	ToLane   int
	ToRow    int
	Type     EdgeType
}
 
type Commit struct {
	SHA        string
	ShortSHA   string
	Author     Identity
	Committer  Identity
	Timestamp  time.Time
	Subject    string
	Body       string
	ParentSHAs []string
	ChildSHAs  []string 
	Refs       []string
 
	Lane  int
	Row   int
	Edges []Edge

	Passthrough uint64
}
 
type Row struct {
	Commit      *Commit
	ActiveLanes int 
}
 
type Graph struct {
	mu          sync.RWMutex
	Rows        []*Row
	BySHA       map[string]*Commit
	RefIndex    map[string]string
	CommitRefs  map[string][]string
	TotalLanes  int
}

func (g *Graph) Lock()    { g.mu.Lock() }
func (g *Graph) Unlock()  { g.mu.Unlock() }
func (g *Graph) RLock()   { g.mu.RLock() }
func (g *Graph) RUnlock() { g.mu.RUnlock() }
 
type LaneSlot struct {
	SHA      string
	Colour    int
	IdleSince int
}

type LaneTable struct {
	Slots      []LaneSlot
	ColourCycle int
	RowIndex   int
}
 