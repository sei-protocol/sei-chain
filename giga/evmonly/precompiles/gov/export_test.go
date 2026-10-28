package gov

import "testing"

// SubmittedContent submits proposal JSON and returns the content the proposal
// query gives for it.
func SubmittedContent(t *testing.T, content string) []byte {
	h := newHarness(t)
	return h.proposal(h.submit(alice, content, nil)).Content
}
