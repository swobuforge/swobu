package ui

import (
	"sort"
	"strings"
	"unicode"
)

const (
	scorePrefix    = 400
	scoreSubstring = 300
	scoreKeyword   = 200
	scoreToken     = 100
	scoreLengthMax = 1000
)

func filterSearchOptions(options []SearchOption, query string) []SearchOption {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return append([]SearchOption(nil), options...)
	}

	type scoredOption struct {
		option SearchOption
		score  int
		index  int
	}

	var scored []scoredOption
	for i, opt := range options {
		score := scoreSearchOption(opt, q)
		if score > 0 {
			scored = append(scored, scoredOption{option: opt, score: score, index: i})
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].index < scored[j].index
	})

	result := make([]SearchOption, len(scored))
	for i, s := range scored {
		result[i] = s.option
	}
	return result
}

func scoreSearchOption(opt SearchOption, q string) int {
	labelLower := strings.ToLower(opt.Label)
	score := 0

	switch {
	case strings.HasPrefix(labelLower, q):
		score = scorePrefix
	case strings.Contains(labelLower, q):
		score = scoreSubstring
	default:
		for _, kw := range opt.Keywords {
			if strings.Contains(strings.ToLower(kw), q) {
				score = scoreKeyword
				break
			}
		}
		if score == 0 && tokenSubsequenceMatch(labelLower, q) {
			score = scoreToken
		}
	}

	if score > 0 {
		score += scoreLengthMax - len(opt.Label)
	}
	return score
}

func tokenSubsequenceMatch(label, query string) bool {
	labelTokens := tokenize(label)
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return true
	}
	if len(queryTokens) > len(labelTokens) {
		return false
	}

	qIdx := 0
	for _, t := range labelTokens {
		if strings.HasPrefix(t, queryTokens[qIdx]) {
			qIdx++
			if qIdx >= len(queryTokens) {
				return true
			}
		}
	}
	return false
}

func tokenize(s string) []string {
	var tokens []string
	for _, word := range strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if word != "" {
			tokens = append(tokens, strings.ToLower(word))
		}
	}
	return tokens
}
