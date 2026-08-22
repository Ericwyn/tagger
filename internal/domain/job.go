package domain

import "time"

type JobKind string
type JobState string

const (
	JobScan  JobKind = "scan"
	JobMatch JobKind = "match"
	JobWrite JobKind = "write"

	JobWaiting   JobState = "waiting"
	JobRunning   JobState = "running"
	JobReview    JobState = "review"
	JobSucceeded JobState = "succeeded"
	JobPartial   JobState = "partial"
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
)

type Job struct {
	ID          string
	Kind        JobKind
	State       JobState
	LibraryID   string
	Title       string
	Detail      string
	Processed   int
	Total       int
	Succeeded   int
	Failed      int
	Error       string
	Payload     string
	CreatedAt   time.Time
	StartedAt   time.Time
	CompletedAt time.Time
	UpdatedAt   time.Time
}
