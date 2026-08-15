package model

import (
	"strings"
	"time"
)

// RecentActivity represents a past conversational session summary.
type RecentActivity struct {
	externalID string
	title      string
	startTime  time.Time
}

// NewRecentActivity creates a validated RecentActivity instance.
func NewRecentActivity(externalID, title string, startTime time.Time) (RecentActivity, error) {
	trimmed := strings.TrimSpace(externalID)
	if trimmed == "" {
		return RecentActivity{}, ErrInvalidExternalID
	}
	if startTime.IsZero() {
		startTime = time.Now().UTC()
	}
	return RecentActivity{
		externalID: trimmed,
		title:      strings.TrimSpace(title),
		startTime:  startTime,
	}, nil
}

// ExternalID returns the session external ID.
func (r RecentActivity) ExternalID() string {
	return r.externalID
}

// Title returns the descriptive activity title.
func (r RecentActivity) Title() string {
	return r.title
}

// StartTime returns when the session started.
func (r RecentActivity) StartTime() time.Time {
	return r.startTime
}
