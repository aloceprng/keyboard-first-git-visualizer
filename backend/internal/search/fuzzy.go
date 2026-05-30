package search

import (
	"path/filepath"
	"sort"
	"strings"
)

type Score struct {
	TrigramDensity float64
	PrefixBonus    float64
	ExactBonus     float64
	RecencyWeight  float64
	TypoDistance   int
	Total          float64
}

var scoreWeights = struct {
	TrigramDensity float64
	PrefixBonus    float64
	ExactBonus     float64
	RecencyWeight  float64
	TypoPenalty    float64
}{
	TrigramDensity: 0.40,
	PrefixBonus:    0.25,
	ExactBonus:     0.20,
	RecencyWeight:  0.10,
	TypoPenalty:    0.05,
}

// computes a Score for one commit SHA against the normalised query
func (idx *Index) ScoreCandidate(sha string, query string, queryTrigrams []string, row int, totalRows int, kind string) Score {
	return idx.scoreByKind(sha, query, kind, queryTrigrams, row, totalRows)
}

// sorts results descending by Score.Total, applies the limit, and returns the final slice
func RankResults(results []Result, limit int) []Result {
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score.Total > results[j].Score.Total
	})

	if limit > 0 && len(results) > limit {
		return results[:limit]
	}

	return results
}

// lowercases query and strips leading & trailing whitespace
func normalizeQuery(q string) string { return strings.ToLower(strings.TrimSpace(q)) }

// return 1.0 if query is a prefix of target, 0.5 if query is a prefix of any whitespace-delimited word in target, else 0
func prefixScore(query, target string) float64 {
	if strings.HasPrefix(target, query) { return 1.0 }
	
	for _, w := range strings.Fields(target) {
		if strings.HasPrefix(w, query) { return 0.5 }
	}

	return 0
}

// returns 1.0 if query == target exactly (case-insensitive), else 0
func exactScore(query, target string) float64 {
	if query == target { return 1.0 }

	return 0
}

// returns the fraction of queryTrigrams found in targetTrigrams
func trigramDensity(queryTrigrams, targetTrigrams []string) float64 {
	if len(queryTrigrams) == 0 { return 0 }

	set := make(map[string]struct{}, len(targetTrigrams))
	for _, t := range targetTrigrams {
		set[t] = struct{}{}
	}

	var hits float64
	for _, t := range queryTrigrams {
		if _, ok := set[t]; ok {
			hits++
		}
	}

	return hits / float64(len(queryTrigrams))
}

// computes the Levenshtein distance between a and b, used for typo tolerance
// only when trigramDensity is below threshold
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)

	if la == 0 { return lb }
	if lb == 0 { return la }

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(
				prev[j]+1,
				curr[j-1]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}

	return prev[lb]
}

// finds the minimum edit distance between query and any substring with the same length
func bestSubstringDistance(query, target string) int {
	rq, rt := []rune(query), []rune(target)
	lq, lt := len(rq), len(rt)

	if lq == 0 { return 0 }
	if lt < lq { return editDistance(query, target) }

	best := lq

	for i := 0; i <= lt-lq; i++ {
		d := editDistance(string(rq), string(rt[i:i+lq]))
		if d < best { best = d }
		if best == 0 { break }
	}

	return best
}

// maps a commit's row index to a [0, 1] float where row 0 (most recent) = 1.0 and the oldest commit = 0.0
func recencyWeight(row, total int) float64 {
	if total <= 1 { return 1.0 }
	return 1.0 - float64(row)/float64(total-1)
}

// returns [start, end] byte offset pairs for every substring of text that matches a trigram from queryTrigrams
func computeHighlights(text, query string, trigrams []string) []int {
	lower := strings.ToLower(text)
	var runs [][2]int

	start := 0
	for {
		i := strings.Index(lower[start:], query)
		if i < 0 { break }
		abs := start + i
		runs = append(runs, [2]int{abs, abs + len(query)})
		start = abs + len(query)
	}

	for _, t := range trigrams {
		pos := 0
		for {
			i := strings.Index(lower[pos:], t)
			if i < 0 { break }
			abs := pos + i
			runs = append(runs, [2]int{abs, abs + len(t)})
			pos = abs + len(t)
		}
	}

	if len(runs) == 0 { return nil }

	sort.Slice(runs, func(i, j int) bool {
		if runs[i][0] != runs[j][0] {
			return runs[i][0] < runs[j][0]
		}
		return runs[i][1] < runs[j][1]
	})

	return mergeRuns(runs)
}

// collapses overlapping or adjacent [start, end] pairs into the minimal set of covering ranges
func mergeRuns(runs [][2]int) []int {
	if len(runs) == 0 { return nil }

	merged := runs[:1:len(runs)]

	for _, r := range runs[1:] {
		last := &merged[len(merged)-1]
		if r[0] <= last[1] {
			if r[1] > last[1] {
				last[1] = r[1]
			}
		} else {
			merged = append(merged, r)
		}
	}

	out := make([]int, 0, len(merged)*2)
	for _, r := range merged {
		out = append(out, r[0], r[1])
	}

	return out
}

// applies kind-specific field selection before scoring
func (idx *Index) scoreByKind( sha, query, kind string, queryTrigrams []string, row int, totalRows int) Score {
	c, ok := idx.commits[sha]
	if !ok { return Score{} }

	var target string

	switch kind {
	case "author":
		target = strings.ToLower(c.Author.Name + " " + c.Author.Email)

	case "file":
		target = strings.ToLower(filepath.Base(c.Subject)) // fallback to subject

	default:
		target = strings.ToLower(c.Subject + " " + c.Body)
	}

	targetTrigrams := extractTrigrams(target)

	td := trigramDensity(queryTrigrams, targetTrigrams)
	pb := prefixScore(query, target)
	eb := exactScore(query, target)

	typo := 0
	if td < 0.4 && len(query) >= 3 {
		typo = bestSubstringDistance(query, target)
	}

	rec := recencyWeight(row, totalRows)

	typoScore := 0.0
	if typo == 0 {
		typoScore = scoreWeights.TypoPenalty
	} else {
		reduction := float64(typo) / float64(len([]rune(query)))
		typoScore = scoreWeights.TypoPenalty * (1.0 - reduction)
		if typoScore < 0 {
			typoScore = 0
		}
	}

	total :=
		td*scoreWeights.TrigramDensity +
			pb*scoreWeights.PrefixBonus +
			eb*scoreWeights.ExactBonus +
			rec*scoreWeights.RecencyWeight +
			typoScore

	return Score{
		TrigramDensity: td,
		PrefixBonus: pb,
		ExactBonus: eb,
		RecencyWeight: rec,
		TypoDistance: typo,
		Total: total,
	}
}

// weights the filename component of a path higher than parent directories
func filePathScore(query, filePath string) float64 {
	base := strings.ToLower(filepath.Base(filePath))
	if strings.HasPrefix(base, query) {
		return 1.0
	}
	return trigramDensity(extractTrigrams(query), extractTrigrams(base))
}

// returns mininmum of 3 ints
func min3(a, b, c int) int {
	if a < b && a < c { return a }
	if b < c { return b }
	return c
}