package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// ConfidenceLevel describes the likelihood of a match.
type ConfidenceLevel string

const (
	ConfidenceHigh   ConfidenceLevel = "HIGH"
	ConfidenceMedium ConfidenceLevel = "MEDIUM"
	ConfidenceLow    ConfidenceLevel = "LOW"
	ConfidenceNone   ConfidenceLevel = "NONE"
)

// Source represents an enumerated marketplace source.
type Source string

const (
	SourceSSCom        Source = "ss.com"
	SourceVitaLombards Source = "vitalombards"
	SourceAndele       Source = "andele"
	SourceBanknote     Source = "banknote"
	SourceVinted       Source = "vinted"
	SourceFacebook     Source = "facebook"
	SourceUnknown      Source = "unknown"
)

// Listing represents a normalized marketplace listing across all sources.
type Listing struct {
	ID            string          `json:"id"`
	Source        Source          `json:"source"`
	SourceID      string          `json:"source_id"`
	URL           string          `json:"url"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	Price         float64         `json:"price"`
	Currency      string          `json:"currency"`
	ImageURLs     []string        `json:"image_urls"`
	Location      string          `json:"location"`
	Seller        string          `json:"seller"`
	Score         int             `json:"score"`
	Confidence    ConfidenceLevel `json:"confidence"`
	MatchReasons  []string        `json:"match_reasons"`
	FirstSeenAt   time.Time       `json:"first_seen_at"`
	LastSeenAt    time.Time       `json:"last_seen_at"`
	Notified      bool            `json:"notified"`
	NotifiedAt    *time.Time      `json:"notified_at,omitempty"`
	RawMetadata   map[string]any  `json:"raw_metadata,omitempty"`
}

// GenerateID produces a deterministic identifier for deduplication.
// Prefers source:source_id when source_id is present, otherwise hashes normalized URL + Title.
func (l *Listing) GenerateID() string {
	if l.SourceID != "" {
		return string(l.Source) + ":" + strings.TrimSpace(l.SourceID)
	}

	normURL := strings.TrimSpace(strings.ToLower(l.URL))
	normTitle := strings.TrimSpace(strings.ToLower(l.Title))
	data := string(l.Source) + "|" + normURL + "|" + normTitle
	hash := sha256.Sum256([]byte(data))
	return string(l.Source) + ":" + hex.EncodeToString(hash[:12])
}

// PrimaryImage returns the first available image URL or empty string.
func (l *Listing) PrimaryImage() string {
	if len(l.ImageURLs) > 0 {
		return l.ImageURLs[0]
	}
	return ""
}
