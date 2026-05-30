package search

import (
	"sort"
	"strings"
	"sync"
	"github.com/aloceprng/keyboard-first-git-visualizer/backend/internal/graph"
)
 
type Result struct {
	SHA       string
	Score     Score
	Highlight []int
}

// index rebuilds with every graph update
type Index struct {
	mu sync.RWMutex

	trigrams map[string][]string        // trigram → []SHA
	authors  map[string][]*graph.Commit // author/email → commits
	tokens   []string                   // sorted unique tokens (autocomplete)
	commits  map[string]*graph.Commit   // SHA → commit
}
 
// asynchronously constructs all index structures from the full commit list after graph loads
func Build(commits []*graph.Commit) *Index {
	trigrams := make(map[string][]string)
	authors := make(map[string][]*graph.Commit)
	commitsMap := make(map[string]*graph.Commit)
	tokenSet := make(map[string]struct{})

	for _, c := range commits {
		commitsMap[c.SHA] = c
		authors[c.Author.Email] = append(authors[c.Author.Email], c)
		seenTrigrams := make(map[string]struct{})

		msg := strings.ToLower(c.Subject + " " + c.Body)

		for _, t := range extractTrigrams(msg) {
			if _, ok := seenTrigrams[t]; ok { continue }
			seenTrigrams[t] = struct{}{}
			trigrams[t] = append(trigrams[t], c.SHA)
		}

		for _, w := range strings.Fields(msg) { tokenSet[w] = struct{}{} }
	}

	tokens := make([]string, 0, len(tokenSet))
	for t := range tokenSet { tokens = append(tokens, t) }
	sort.Strings(tokens)

	return &Index{
		trigrams: trigrams,
		authors:  authors,
		tokens:   tokens,
		commits:  commitsMap,
	}
}
 
// replaces index contents in-place after a graph update
func (idx *Index) Rebuild(commits []*graph.Commit) {
	fresh := Build(commits)

	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.trigrams = fresh.trigrams
	idx.authors = fresh.authors
	idx.tokens = fresh.tokens
	idx.commits = fresh.commits
}

// runs a fuzzy query against the index and returns up to limit ranked results
func (idx *Index) Search(query string, kind string, limit int) []Result {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" { return nil }

	qTrigrams := extractTrigrams(query)

	var shas []string

	switch kind {
	case "author":
		for email, commits := range idx.authors {
			if strings.Contains(strings.ToLower(email), query) {
				for _, c := range commits {
					shas = append(shas, c.SHA)
				}
			}
		}

	default:
		if len(qTrigrams) == 0 {
			for sha, c := range idx.commits {
				if strings.Contains(strings.ToLower(c.Subject+" "+c.Body), query) {
					shas = append(shas, sha)
				}
			}
		} else {
			shas = idx.candidates(qTrigrams)
		}
	}

	total := float64(len(qTrigrams))
	if total == 0 { total = 1 }

	results := make([]Result, 0, len(shas))

	for _, sha := range shas {
		c, ok := idx.commits[sha]
		if !ok { continue }

		msg := strings.ToLower(c.Subject + " " + c.Body)

		var score float64

		if strings.Contains(msg, query) {
			score = 1.0
		} else {
			msgTrigrams := extractTrigrams(msg)

			triSet := make(map[string]struct{}, len(msgTrigrams))
			for _, t := range msgTrigrams {
				triSet[t] = struct{}{}
			}

			var hits float64
			for _, t := range qTrigrams {
				if _, ok := triSet[t]; ok {
					hits++
				}
			}

			score = hits / total
		}

		results = append(results, Result{
			SHA: sha,
			Score: Score{Total: score},
			Highlight: idx.highlightRanges(sha, query),
		})
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score.Total > results[j].Score.Total })

	if limit > 0 && len(results) > limit { results = results[:limit] }

	return results
}

// lowercases and returns all 3-char substrings in s
func extractTrigrams(s string) []string { 
	runes := []rune(strings.ToLower(s))
	trigrams := []string{}
	
	if len(runes) >= 3{ 
		for i := 0; i <= len(runes)-3; i++ {
			trigrams = append(trigrams, string(runes[i:i+3]))
		}
	}

    return trigrams
}
 
// return the SHAs present in all trigram hit lists for the query
func (idx *Index) candidates(trigrams []string) []string {
	if len(trigrams) == 0 {
		return nil
	}

	seed := trigrams[0]

	for _, t := range trigrams[1:] {
		if len(idx.trigrams[t]) < len(idx.trigrams[seed]) {
			seed = t
		}
	}

	base := idx.trigrams[seed]
	if len(base) == 0 {
		return nil
	}

	current := make(map[string]struct{}, len(base))
	for _, sha := range base { current[sha] = struct{}{} }

	for _, t := range trigrams {
		if t == seed { continue }

		hits := idx.trigrams[t]
		if len(hits) == 0 { return nil }

		hitSet := make(map[string]struct{}, len(hits))
		for _, sha := range hits { 
			hitSet[sha] = struct{}{} 
		}

		for sha := range current {
			if _, ok := hitSet[sha]; !ok { 
				delete(current, sha) 
			}
		}

		if len(current) == 0 { return nil }
	}

	result := make([]string, 0, len(current))
	for sha := range current {
		result = append(result, sha)
	}

	return result
}
 
// return [start,end] byte offset pairs for substrings of the
// commit subject that match the query, used for bold rendering in the frontend
func (idx *Index) highlightRanges(sha string, query string) []int {
	c, ok := idx.commits[sha]
	if !ok || query == "" { return nil }
	msg := strings.ToLower(c.Subject + " " + c.Body)
	qLen := len(query)

	var ranges []int
	start := 0

	for {
		i := strings.Index(msg[start:], query)
		if i < 0 { break }

		abs := start + i
		ranges = append(ranges, abs, abs+qLen)
		start = abs + qLen
	}

	return ranges
}

// return tokens matching prefix
func (idx *Index) Autocomplete(prefix string, n int) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	prefix = strings.ToLower(prefix)
	if prefix == "" || n <= 0 { return nil }

	lo := sort.SearchStrings(idx.tokens, prefix)
	var results []string

	for i := lo; i < len(idx.tokens) && len(results) < n; i++ {
		if !strings.HasPrefix(idx.tokens[i], prefix) { break }
		results = append(results, idx.tokens[i])
	}

	return results
}