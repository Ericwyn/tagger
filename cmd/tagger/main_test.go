package main

import (
	"testing"

	"github.com/ericwyn/tagger/internal/providers"
)

func TestPatchFromCandidateDistinguishesOmittedAndEmptyFieldLists(t *testing.T) {
	candidate := providers.MatchCandidate{
		Title:     providers.Field[string]{Value: "新标题"},
		DiscTotal: providers.Field[int]{Value: 2},
		Lyrics:    &providers.Field[string]{Value: "歌词"},
	}

	allFields := patchFromCandidate(candidate, nil)
	if allFields.Title == nil || allFields.DiscTotal == nil || allFields.Lyrics == nil {
		t.Fatalf("nil fields should preserve the backwards-compatible all-fields behavior: %#v", allFields)
	}

	noFields := patchFromCandidate(candidate, []string{})
	if noFields.Title != nil || noFields.DiscTotal != nil || noFields.Lyrics != nil {
		t.Fatalf("an explicit empty field list must produce a no-op patch: %#v", noFields)
	}

	selected := patchFromCandidate(candidate, []string{"title"})
	if selected.Title == nil || selected.DiscTotal != nil || selected.Lyrics != nil {
		t.Fatalf("selected fields must be applied precisely: %#v", selected)
	}
}
