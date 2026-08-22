package domain

import "testing"

func TestSidecarRevisionIsContentStable(t *testing.T) {
	first := SidecarRevision([]byte("[00:01.00]hello"))
	if first == "" || first != SidecarRevision([]byte("[00:01.00]hello")) {
		t.Fatalf("revision = %q", first)
	}
	if first == SidecarRevision([]byte("[00:01.00]different")) {
		t.Fatalf("different content reused revision %q", first)
	}
}
