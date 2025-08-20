package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
)

type inputDocument struct {
	ID          string `json:"id"`
	URL         string `json:"url,omitempty"`
	Title       string `json:"title,omitempty"`
	Text        string `json:"text,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

type toolInput struct {
	Documents []inputDocument `json:"docs"`
}

type outputGroup struct {
	RepresentativeID string   `json:"representative_id"`
	Members          []string `json:"members"`
	Score            float64  `json:"score"`
}

type toolOutput struct {
	Groups []outputGroup `json:"groups"`
}

type stderrError struct {
	Error string `json:"error"`
	Hint  string `json:"hint,omitempty"`
}

func writeErrorAndExit(err error, hint string) {
	encErr := json.NewEncoder(os.Stderr).Encode(stderrError{Error: err.Error(), Hint: hint})
	if encErr != nil {
		// Best-effort fallback when JSON encode fails
		_, _ = fmt.Fprintf(os.Stderr, "error=%q hint=%q\n", err.Error(), hint)
	}
	os.Exit(1)
}

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		writeErrorAndExit(err, "failed to read stdin")
		return
	}
	in, err := parseInput(data)
	if err != nil {
		writeErrorAndExit(err, "invalid JSON input for dedupe_rank")
		return
	}
	if len(in.Documents) == 0 {
		writeErrorAndExit(errors.New("missing docs"), "provide docs: [{id,title?,text?,url?,published_at?}]")
		return
	}

	documents := buildDocuments(in)
	groups := groupDocuments(documents, 0.25)
	out := toolOutput{Groups: groups}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", "failed to encode output")
		os.Exit(1)
	}
}

// parseInput unmarshals tool input from raw JSON bytes.
func parseInput(data []byte) (toolInput, error) {
	var in toolInput
	err := json.Unmarshal(data, &in)
	return in, err
}

type docData struct {
	doc    inputDocument
	tokens []string
	set    map[string]struct{}
}

// buildDocuments tokenizes, filters, and constructs set representations.
func buildDocuments(in toolInput) []docData {
	documents := make([]docData, 0, len(in.Documents))
	for _, d := range in.Documents {
		tokens := tokenizeWords(strings.TrimSpace(d.Title + " " + d.Text))
		tokens = filterStopwords(tokens)
		set := make(map[string]struct{}, len(tokens))
		for _, s := range tokens {
			set[s] = struct{}{}
		}
		documents = append(documents, docData{doc: d, tokens: tokens, set: set})
	}
	return documents
}

// groupDocuments performs similarity grouping and representative selection.
func groupDocuments(documents []docData, jaccardThreshold float64) []outputGroup {
	// Union-Find structure
	parent := make([]int, len(documents))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}

	// Pairwise similarities
	for i := 0; i < len(documents); i++ {
		for j := i + 1; j < len(documents); j++ {
			sim := jaccard(documents[i].set, documents[j].set)
			if sim >= jaccardThreshold {
				union(i, j)
			}
		}
	}

	// Build groups by root parent
	rootToIdx := make(map[int][]int)
	for i := range documents {
		r := find(i)
		rootToIdx[r] = append(rootToIdx[r], i)
	}

	// Compute token doc frequency for TF-IDF scoring
	tokenDocFreq := make(map[string]int)
	for _, dd := range documents {
		seen := map[string]struct{}{}
		for _, t := range dd.tokens {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			tokenDocFreq[t]++
		}
	}
	scorer := func(idx int) float64 { return tfidfScore(documents[idx].tokens, tokenDocFreq, float64(len(documents))) }

	groups := make([]outputGroup, 0, len(rootToIdx))
	for _, idxs := range rootToIdx {
		if len(idxs) == 1 {
			i := idxs[0]
			groups = append(groups, outputGroup{
				RepresentativeID: documents[i].doc.ID,
				Members:          []string{documents[i].doc.ID},
				Score:            0,
			})
			continue
		}
		// Best representative by score; tie-break by id
		bestIdx := idxs[0]
		bestScore := scorer(bestIdx)
		for k := 1; k < len(idxs); k++ {
			s := scorer(idxs[k])
			if s > bestScore || (s == bestScore && documents[idxs[k]].doc.ID < documents[bestIdx].doc.ID) {
				bestScore = s
				bestIdx = idxs[k]
			}
		}
		members := make([]string, 0, len(idxs))
		for _, i := range idxs {
			members = append(members, documents[i].doc.ID)
		}
		sort.Strings(members)
		groups = append(groups, outputGroup{
			RepresentativeID: documents[bestIdx].doc.ID,
			Members:          members,
			Score:            bestScore,
		})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].RepresentativeID < groups[j].RepresentativeID })
	return groups
}

// tfidfScore computes a crude TF-IDF score for a token sequence.
func tfidfScore(tokens []string, tokenDocFreq map[string]int, numDocs float64) float64 {
	tf := map[string]int{}
	for _, t := range tokens {
		tf[t]++
	}
	var score float64
	for tok, c := range tf {
		df := float64(tokenDocFreq[tok])
		idf := 0.0
		if df > 0 {
			idf = math.Log(numDocs / df)
		}
		score += (1.0 + math.Log(float64(c))) * idf
	}
	return score
}

// tokenizeWords splits text into lowercase alphanumeric tokens.
func tokenizeWords(s string) []string {
	// Replace non-letters with spaces, split on spaces
	b := strings.Builder{}
	b.Grow(len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	parts := strings.Fields(strings.ToLower(b.String()))
	return parts
}

// filterStopwords removes a small set of common English stopwords.
func filterStopwords(tokens []string) []string {
	if len(tokens) == 0 {
		return tokens
	}
	stop := map[string]struct{}{
		"a": {}, "an": {}, "the": {}, "is": {}, "are": {}, "was": {}, "were": {},
		"by": {}, "of": {}, "and": {}, "to": {}, "in": {}, "on": {}, "for": {},
		"with": {}, "as": {}, "it": {}, "its": {}, "at": {}, "this": {}, "that": {},
	}
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if _, ok := stop[t]; ok {
			continue
		}
		out = append(out, t)
	}
	return out
}

// jaccard computes Jaccard similarity between two sets.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	inter := 0
	var small, large map[string]struct{}
	if len(a) < len(b) {
		small, large = a, b
	} else {
		small, large = b, a
	}
	for k := range small {
		if _, ok := large[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
