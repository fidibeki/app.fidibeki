package moderation

import "testing"

func TestRedactCopiesPreservesIndependentSummaries(t *testing.T) {
	t.Parallel()
	unsafe := "an abusive raw report"
	if got := RedactCopies("FB-1: "+unsafe, unsafe); got != "FB-1: "+Placeholder {
		t.Errorf("RedactCopies(copied) = %q", got)
	}
	if got := RedactCopies("A safe operator summary", unsafe); got != "A safe operator summary" {
		t.Errorf("RedactCopies(independent) = %q", got)
	}
	if got := RedactCopies(unsafe, "", unsafe, unsafe); got != Placeholder {
		t.Errorf("RedactCopies(duplicates) = %q", got)
	}
}

func TestRedactDerivedCatchesFallbackSubstrings(t *testing.T) {
	t.Parallel()
	unsafe := "an abusive raw report with more detail"
	if got := RedactDerived("an abusive raw report", unsafe); got != Placeholder {
		t.Errorf("RedactDerived(substring) = %q", got)
	}
	if got := RedactDerived("FB-1: "+unsafe, unsafe); got != "FB-1: "+Placeholder {
		t.Errorf("RedactDerived(wrapped) = %q", got)
	}
	if got := RedactDerived("A safe operator summary", unsafe); got != "A safe operator summary" {
		t.Errorf("RedactDerived(independent) = %q", got)
	}
}
