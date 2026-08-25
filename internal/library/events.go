package library

import (
	"sync"

	"github.com/ericwyn/tagger/internal/domain"
)

type EventKind string

const (
	EventSnapshot  EventKind = "snapshot"
	EventInventory EventKind = "inventory"
	EventMetadata  EventKind = "metadata"
	EventWatcher   EventKind = "watcher-state"
)

// Event invalidates a recoverable library snapshot. Consumers use Generation
// for de-duplication and always re-fetch authoritative REST data after an event.
type Event struct {
	LibraryID  string            `json:"libraryId"`
	Generation uint64            `json:"generation"`
	Kind       EventKind         `json:"kind"`
	Paths      []string          `json:"paths,omitempty"`
	WatchMode  domain.WatchMode  `json:"watchMode,omitempty"`
	WatchState domain.WatchState `json:"watchState,omitempty"`
}

func (s *Service) SubscribeEvents() (<-chan Event, func()) {
	channel := make(chan Event, 16)
	s.eventMu.Lock()
	if s.eventSubs == nil {
		s.eventSubs = make(map[chan Event]struct{})
	}
	s.eventSubs[channel] = struct{}{}
	generation := s.eventVersion
	s.eventMu.Unlock()
	library := s.Library()
	channel <- Event{
		LibraryID: library.ID, Generation: generation, Kind: EventSnapshot,
		WatchMode: library.WatchMode, WatchState: library.WatchState,
	}
	var once sync.Once
	return channel, func() {
		once.Do(func() {
			s.eventMu.Lock()
			if _, found := s.eventSubs[channel]; found {
				delete(s.eventSubs, channel)
				close(channel)
			}
			s.eventMu.Unlock()
		})
	}
}

func (s *Service) publishEvent(event Event) {
	library := s.Library()
	s.eventMu.Lock()
	s.eventVersion++
	if s.eventVersion == 0 {
		s.eventVersion = 1
	}
	event.LibraryID = library.ID
	event.Generation = s.eventVersion
	event.WatchMode = library.WatchMode
	event.WatchState = library.WatchState
	event.Paths = append([]string(nil), event.Paths...)
	for channel := range s.eventSubs {
		select {
		case channel <- event:
		default:
		}
	}
	s.eventMu.Unlock()
}

func (s *Service) SetWatchStatus(mode domain.WatchMode, state domain.WatchState) {
	s.mu.Lock()
	changed := s.library.WatchMode != mode || s.library.WatchState != state
	s.library.WatchMode = mode
	s.library.WatchState = state
	s.mu.Unlock()
	if changed {
		s.publishEvent(Event{Kind: EventWatcher})
	}
}

func (s *Service) EventGeneration() uint64 {
	s.eventMu.RLock()
	defer s.eventMu.RUnlock()
	return s.eventVersion
}
