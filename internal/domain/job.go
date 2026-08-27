package domain

import "time"

type JobKind string
type JobState string

const (
	JobScan      JobKind = "scan"
	JobMatch     JobKind = "match"
	JobWrite     JobKind = "write"
	JobBatchEdit JobKind = "batch_edit"

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

// IsMatchReviewable reports whether a persisted matching workflow can still
// be opened to inspect, rematch, skip, or resubmit items. Failed and partial
// workflows remain reviewable so a write permission fix does not force users
// to discard otherwise valid candidates.
func IsMatchReviewable(job Job) bool {
	if job.Kind != JobMatch {
		return false
	}
	switch job.State {
	case JobReview, JobPartial, JobFailed:
		return true
	default:
		return false
	}
}
