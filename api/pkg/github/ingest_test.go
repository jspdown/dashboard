package github

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jspdown/dashboard/api/pkg/pullrequest"
)

func TestNormalizeReviewState(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		// REST API: GitHub returns uppercase.
		{"APPROVED", pullrequest.VerdictApproved},
		{"CHANGES_REQUESTED", pullrequest.VerdictChangesRequested},
		{"DISMISSED", pullrequest.VerdictDismissed},
		{"COMMENTED", pullrequest.VerdictCommented},
		// Webhook payload: lowercase.
		{"approved", pullrequest.VerdictApproved},
		{"changes_requested", pullrequest.VerdictChangesRequested},
		{"dismissed", pullrequest.VerdictDismissed},
		{"commented", pullrequest.VerdictCommented},
		// REST review-creation event uses "request_changes".
		{"request_changes", pullrequest.VerdictChangesRequested},
		{"REQUEST_CHANGES", pullrequest.VerdictChangesRequested},
		// Anything else is treated as a plain comment.
		{"", pullrequest.VerdictCommented},
		{"pending", pullrequest.VerdictCommented},
	}
	for _, tc := range tests {
		t.Run(tc.state, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeReviewState(tc.state))
		})
	}
}
