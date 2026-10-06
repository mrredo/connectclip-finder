package matcher

import (
	"connectclip-finder/internal/model"
	"strings"
)

// Matcher performs deterministic scoring and confidence calculation.
type Matcher struct {
	rules []Rule
}

// NewMatcher creates a new deterministic matching engine.
func NewMatcher() *Matcher {
	return &Matcher{
		rules: BuildRules(),
	}
}

// Evaluate analyzes a listing, populates its Score, Confidence, and MatchReasons.
func (m *Matcher) Evaluate(listing *model.Listing) {
	normTitle := NormalizeText(listing.Title)
	normDesc := NormalizeText(listing.Description)

	// If description is empty or very short, use title for both
	if strings.TrimSpace(normDesc) == "" {
		normDesc = normTitle
	}

	score := 0
	var reasons []string

	for _, rule := range m.rules {
		matched, reason := rule.Check(normTitle, normDesc, listing.Price)
		if matched {
			score += rule.Weight
			if reason != "" {
				reasons = append(reasons, reason)
			}
		}
	}

	// Clamp score between 0 and 100
	if score < 0 {
		score = 0
	} else if score > 100 {
		score = 100
	}

	listing.Score = score
	listing.MatchReasons = reasons

	switch {
	case score >= 75:
		listing.Confidence = model.ConfidenceHigh
	case score >= 50:
		listing.Confidence = model.ConfidenceMedium
	case score >= 25:
		listing.Confidence = model.ConfidenceLow
	default:
		listing.Confidence = model.ConfidenceNone
	}
}
