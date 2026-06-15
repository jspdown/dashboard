package github

import (
	"context"
	"strings"
	"time"

	gh "github.com/google/go-github/v85/github"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jspdown/dashboard/api/pkg/postgres"
	"github.com/jspdown/dashboard/api/pkg/pullrequest"
)

type Ingester struct {
	pool *pgxpool.Pool
	prs  *pullrequest.Store
}

func NewIngester(pool *pgxpool.Pool, prs *pullrequest.Store) *Ingester {
	return &Ingester{pool: pool, prs: prs}
}

func (i *Ingester) Apply(ctx context.Context, event any) error {
	return postgres.BeginTxFunc(ctx, i.pool, pgx.TxOptions{}, func(ctx context.Context) error {
		switch e := event.(type) {
		case *gh.PullRequestEvent:
			return i.handlePR(ctx, e)
		case *gh.PullRequestReviewEvent:
			return i.handleReview(ctx, e)
		case *gh.IssueCommentEvent:
			return i.handleIssueComment(ctx, e)
		case *gh.CheckRunEvent:
			return i.handleCheckRun(ctx, e)
		default:
			return nil
		}
	})
}

func (i *Ingester) handlePR(ctx context.Context, e *gh.PullRequestEvent) error {
	pr := e.GetPullRequest()
	if pr == nil {
		return nil
	}

	row := pullRequestRow(e.GetRepo(), pr)
	if err := i.prs.UpsertPullRequest(ctx, row); err != nil {
		return err
	}

	if err := i.prs.ReplaceLabels(ctx, row.GithubID, labelNames(pr)); err != nil {
		return err
	}

	reviewers := requestedReviewerLogins(pr)
	return i.prs.ReplaceReviewRequests(ctx, row.GithubID, reviewers)
}

func (i *Ingester) handleReview(ctx context.Context, e *gh.PullRequestReviewEvent) error {
	pr := e.GetPullRequest()
	if pr != nil {
		row := pullRequestRow(e.GetRepo(), pr)
		if err := i.prs.UpsertPullRequest(ctx, row); err != nil {
			return err
		}
	}

	review := e.GetReview()
	if review == nil {
		return nil
	}

	verdict := normalizeReviewState(review.GetState())
	if err := i.prs.UpsertReview(ctx,
		review.GetID(),
		pr.GetID(),
		review.GetUser().GetLogin(),
		verdict,
		review.GetSubmittedAt().Time,
	); err != nil {
		return err
	}

	if verdict == "approved" || verdict == "changes_requested" {
		return i.prs.RemoveReviewRequest(ctx, pr.GetID(), review.GetUser().GetLogin())
	}
	return nil
}

func (i *Ingester) handleIssueComment(ctx context.Context, e *gh.IssueCommentEvent) error {
	issue := e.GetIssue()
	if issue == nil || issue.PullRequestLinks == nil {
		return nil
	}
	return i.prs.UpdateCommentsCountByNumber(ctx, e.GetRepo().GetFullName(), issue.GetNumber(), issue.GetComments())
}

func (i *Ingester) handleCheckRun(ctx context.Context, e *gh.CheckRunEvent) error {
	cr := e.GetCheckRun()
	if cr == nil {
		return nil
	}

	var conclusion *string
	if c := cr.GetConclusion(); c != "" {
		conclusion = &c
	}
	var completedAt *time.Time
	if t := cr.GetCompletedAt(); !t.IsZero() {
		tt := t.Time
		completedAt = &tt
	}

	return i.prs.UpsertCheckRun(ctx,
		cr.GetID(),
		e.GetRepo().GetFullName(),
		cr.GetHeadSHA(),
		cr.GetName(),
		cr.GetStatus(),
		conclusion,
		completedAt,
	)
}

func pullRequestRow(repo *gh.Repository, pr *gh.PullRequest) pullrequest.Row {
	row := pullrequest.Row{
		GithubID:      pr.GetID(),
		NodeID:        pr.GetNodeID(),
		Repo:          repo.GetFullName(),
		PRNumber:      pr.GetNumber(),
		Title:         pr.GetTitle(),
		Author:        pr.GetUser().GetLogin(),
		Status:        prStatus(pr),
		Draft:         pr.GetDraft(),
		Additions:     pr.GetAdditions(),
		Deletions:     pr.GetDeletions(),
		CommentsCount: pr.GetComments() + pr.GetReviewComments(),
		HeadSHA:       pr.GetHead().GetSHA(),
		CreatedAt:     pr.GetCreatedAt().Time,
		UpdatedAt:     pr.GetUpdatedAt().Time,
		IngestVersion: IngestVersion,
	}
	if t := pr.GetClosedAt(); !t.IsZero() {
		v := t.Time
		row.ClosedAt = &v
	}
	if t := pr.GetMergedAt(); !t.IsZero() {
		v := t.Time
		row.MergedAt = &v
	}
	if u := pr.GetMergedBy().GetLogin(); u != "" {
		row.MergedBy = &u
	}
	return row
}

func prStatus(pr *gh.PullRequest) string {
	if t := pr.GetMergedAt(); !t.IsZero() {
		return pullrequest.StatusMerged
	}
	if pr.GetState() == "closed" {
		return pullrequest.StatusClosed
	}
	return pullrequest.StatusOpen
}

func labelNames(pr *gh.PullRequest) []string {
	out := make([]string, 0, len(pr.Labels))
	for _, l := range pr.Labels {
		if name := l.GetName(); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func requestedReviewerLogins(pr *gh.PullRequest) []string {
	out := make([]string, 0, len(pr.RequestedReviewers))
	for _, u := range pr.RequestedReviewers {
		if login := u.GetLogin(); login != "" {
			out = append(out, login)
		}
	}
	return out
}

// normalizeReviewState maps a GitHub review state to our canonical verdict. The
// REST API returns it uppercase ("APPROVED") and webhooks lowercase, so we
// lowercase first and accept both.
func normalizeReviewState(state string) string {
	switch strings.ToLower(state) {
	case "approved":
		return pullrequest.VerdictApproved
	case "changes_requested", "request_changes":
		return pullrequest.VerdictChangesRequested
	case "dismissed":
		return pullrequest.VerdictDismissed
	default:
		return pullrequest.VerdictCommented
	}
}
