package model

import "time"

// SourceStatus tracks the execution result of an individual marketplace adapter.
type SourceStatus struct {
	Source        Source        `json:"source"`
	Duration      time.Duration `json:"duration"`
	ListingsFound int           `json:"listings_found"`
	Error         string        `json:"error,omitempty"`
}

// ScanReport summarizes an entire monitoring cycle.
type ScanReport struct {
	ID                 int64          `json:"id"`
	StartedAt          time.Time      `json:"started_at"`
	CompletedAt        time.Time      `json:"completed_at"`
	Duration           time.Duration  `json:"duration"`
	TotalSources       int            `json:"total_sources"`
	FailedSources      int            `json:"failed_sources"`
	ListingsDiscovered int            `json:"listings_discovered"`
	NewListings        int            `json:"new_listings"`
	CandidatesFound    int            `json:"candidates_found"`
	NotificationsSent  int            `json:"notifications_sent"`
	SourceStatuses     []SourceStatus `json:"source_statuses"`
}
