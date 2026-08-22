package jobs

import "github.com/ericwyn/tagger/internal/domain"

// Event is a monotonic in-process snapshot notification. The persisted job
// row remains the source of truth when a client reconnects or an event was
// dropped for a slow subscriber.
type Event struct {
	ID  string
	Job domain.Job
}
