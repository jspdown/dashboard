package pullrequest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jspdown/dashboard/api/pkg/postgres"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type Row struct {
	GithubID      int64      `db:"github_id"`
	NodeID        string     `db:"node_id"`
	Repo          string     `db:"repo"`
	PRNumber      int        `db:"pr_number"`
	Title         string     `db:"title"`
	Author        string     `db:"author"`
	Status        string     `db:"status"`
	Draft         bool       `db:"draft"`
	Additions     int        `db:"additions"`
	Deletions     int        `db:"deletions"`
	CommentsCount int        `db:"comments_count"`
	HeadSHA       string     `db:"head_sha"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
	ClosedAt      *time.Time `db:"closed_at"`
	MergedAt      *time.Time `db:"merged_at"`
	MergedBy      *string    `db:"merged_by"`
	// IngestVersion is stamped by the ingester to record which schema-shape
	// this row was written under. The poller uses it to backfill rows on
	// startup after a version bump.
	IngestVersion int `db:"ingest_version"`
}

func (s *Store) UpsertPullRequest(ctx context.Context, pr Row) error {
	const query = `
INSERT INTO pull_requests (
    github_id, node_id, repo, pr_number, title, author, status, draft,
    additions, deletions, comments_count, head_sha,
    created_at, updated_at, closed_at, merged_at, merged_by, ingest_version, synced_at
) VALUES (
    @github_id, @node_id, @repo, @pr_number, @title, @author, @status, @draft,
    @additions, @deletions, @comments_count, @head_sha,
    @created_at, @updated_at, @closed_at, @merged_at, @merged_by, @ingest_version, now()
)
ON CONFLICT (github_id) DO UPDATE SET
    node_id        = EXCLUDED.node_id,
    title          = EXCLUDED.title,
    author         = EXCLUDED.author,
    status         = EXCLUDED.status,
    draft          = EXCLUDED.draft,
    additions      = EXCLUDED.additions,
    deletions      = EXCLUDED.deletions,
    comments_count = EXCLUDED.comments_count,
    head_sha       = EXCLUDED.head_sha,
    updated_at     = EXCLUDED.updated_at,
    closed_at      = EXCLUDED.closed_at,
    merged_at      = EXCLUDED.merged_at,
    merged_by      = EXCLUDED.merged_by,
    ingest_version = EXCLUDED.ingest_version,
    synced_at      = now()
WHERE pull_requests.updated_at <= EXCLUDED.updated_at`

	args := pgx.NamedArgs{
		"github_id":      pr.GithubID,
		"node_id":        pr.NodeID,
		"repo":           pr.Repo,
		"pr_number":      pr.PRNumber,
		"title":          pr.Title,
		"author":         pr.Author,
		"status":         pr.Status,
		"draft":          pr.Draft,
		"additions":      pr.Additions,
		"deletions":      pr.Deletions,
		"comments_count": pr.CommentsCount,
		"head_sha":       pr.HeadSHA,
		"created_at":     pr.CreatedAt,
		"updated_at":     pr.UpdatedAt,
		"closed_at":      pr.ClosedAt,
		"merged_at":      pr.MergedAt,
		"merged_by":      pr.MergedBy,
		"ingest_version": pr.IngestVersion,
	}
	if _, err := postgres.Exec(ctx, s.pool, query, []any{args}); err != nil {
		return fmt.Errorf("upsert pull request: %w", err)
	}
	return nil
}

func (s *Store) ReplaceReviewRequests(ctx context.Context, prGithubID int64, reviewers []string) error {
	return postgres.BeginTxFunc(ctx, s.pool, pgx.TxOptions{}, func(ctx context.Context) error {
		if _, err := postgres.Exec(ctx, s.pool,
			`DELETE FROM pull_request_review_requests WHERE pr_github_id = $1`,
			[]any{prGithubID}); err != nil {
			return fmt.Errorf("clearing review requests: %w", err)
		}

		for _, r := range reviewers {
			if _, err := postgres.Exec(ctx, s.pool,
				`INSERT INTO pull_request_review_requests (pr_github_id, reviewer) VALUES ($1, $2)
				 ON CONFLICT DO NOTHING`,
				[]any{prGithubID, r}); err != nil {
				return fmt.Errorf("inserting review request: %w", err)
			}
		}
		return nil
	})
}

func (s *Store) ReplaceLabels(ctx context.Context, prGithubID int64, labels []string) error {
	return postgres.BeginTxFunc(ctx, s.pool, pgx.TxOptions{}, func(ctx context.Context) error {
		if _, err := postgres.Exec(ctx, s.pool,
			`DELETE FROM pull_request_labels WHERE pr_github_id = $1`,
			[]any{prGithubID}); err != nil {
			return fmt.Errorf("clearing labels: %w", err)
		}

		for _, l := range labels {
			if _, err := postgres.Exec(ctx, s.pool,
				`INSERT INTO pull_request_labels (pr_github_id, label) VALUES ($1, $2)
				 ON CONFLICT DO NOTHING`,
				[]any{prGithubID, l}); err != nil {
				return fmt.Errorf("inserting label: %w", err)
			}
		}
		return nil
	})
}

func (s *Store) RemoveReviewRequest(ctx context.Context, prGithubID int64, reviewer string) error {
	if _, err := postgres.Exec(ctx, s.pool,
		`DELETE FROM pull_request_review_requests WHERE pr_github_id = $1 AND reviewer = $2`,
		[]any{prGithubID, reviewer}); err != nil {
		return fmt.Errorf("removing review request: %w", err)
	}
	return nil
}

func (s *Store) UpsertReview(ctx context.Context, githubReviewID, prGithubID int64, reviewer, verdict string, submittedAt time.Time) error {
	if _, err := postgres.Exec(ctx, s.pool, `
INSERT INTO pull_request_reviews (github_review_id, pr_github_id, reviewer, verdict, submitted_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (github_review_id) DO UPDATE SET
    verdict      = EXCLUDED.verdict,
    submitted_at = EXCLUDED.submitted_at`,
		[]any{githubReviewID, prGithubID, reviewer, verdict, submittedAt}); err != nil {
		return fmt.Errorf("upsert review: %w", err)
	}
	return nil
}

func (s *Store) UpsertCheckRun(ctx context.Context, id int64, repo, headSHA, name, status string, conclusion *string, completedAt *time.Time) error {
	if _, err := postgres.Exec(ctx, s.pool, `
INSERT INTO pull_request_check_runs (github_check_id, repo, head_sha, check_name, run_status, conclusion, completed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (github_check_id) DO UPDATE SET
    repo         = EXCLUDED.repo,
    head_sha     = EXCLUDED.head_sha,
    check_name   = EXCLUDED.check_name,
    run_status   = EXCLUDED.run_status,
    conclusion   = EXCLUDED.conclusion,
    completed_at = EXCLUDED.completed_at`,
		[]any{id, repo, headSHA, name, status, conclusion, completedAt}); err != nil {
		return fmt.Errorf("upsert check run: %w", err)
	}
	return nil
}

func (s *Store) UpdateCommentsCountByNumber(ctx context.Context, repo string, number, count int) error {
	if _, err := postgres.Exec(ctx, s.pool,
		`UPDATE pull_requests SET comments_count = $3, synced_at = now()
		 WHERE repo = $1 AND pr_number = $2`,
		[]any{repo, number, count}); err != nil {
		return fmt.Errorf("update comments count: %w", err)
	}
	return nil
}

// MarkViewed records that userLogin has seen the PR. The snapshot fields
// (comments_count, head_sha) are read from pull_requests in the same
// statement so the baseline is always the server's current truth, never the
// client's. No-op when no PR matches; upserts on repeat calls.
func (s *Store) MarkViewed(ctx context.Context, userLogin string, prGithubID int64) error {
	const query = `
INSERT INTO pull_request_views (user_login, pr_github_id, viewed_at, comments_count_at_view, head_sha_at_view)
SELECT $1, github_id, now(), comments_count, head_sha
FROM pull_requests
WHERE github_id = $2
ON CONFLICT (user_login, pr_github_id) DO UPDATE SET
    viewed_at              = EXCLUDED.viewed_at,
    comments_count_at_view = EXCLUDED.comments_count_at_view,
    head_sha_at_view       = EXCLUDED.head_sha_at_view`

	if _, err := postgres.Exec(ctx, s.pool, query, []any{userLogin, prGithubID}); err != nil {
		return fmt.Errorf("mark viewed: %w", err)
	}
	return nil
}

// ListStaleIngestNumbers returns PR numbers in repo with ingest_version
// below currentVersion that are still active: open, or merged within
// MaxRecentlyMergedDays. The window uses the maximum any user can configure so
// a row stays eligible for backfill regardless of an individual viewer's
// recently-merged setting. The poller calls it to backfill rows after an
// IngestVersion bump.
func (s *Store) ListStaleIngestNumbers(ctx context.Context, repo string, currentVersion int) ([]int, error) {
	const query = `
SELECT pr_number FROM pull_requests
WHERE repo = $1
  AND ingest_version < $2
  AND (status = 'open'
       OR (status = 'merged' AND merged_at > now() - make_interval(days => $3)))
ORDER BY pr_number`

	numbers, err := postgres.QueryMany(ctx, s.pool, query, []any{repo, currentVersion, MaxRecentlyMergedDays}, pgx.RowTo[int])
	if err != nil {
		return nil, fmt.Errorf("listing stale ingest numbers: %w", err)
	}
	return numbers, nil
}

// reviewJSON / checkRunJSON are the shapes the snapshot query's jsonb_agg
// expressions produce. They live here because they're a persistence wire
// format, not a domain concept.
type reviewJSON struct {
	Reviewer    string    `json:"reviewer"`
	Verdict     string    `json:"verdict"`
	SubmittedAt time.Time `json:"submitted_at"`
}

type checkRunJSON struct {
	Name        string     `json:"name"`
	RunStatus   string     `json:"run_status"`
	Conclusion  *string    `json:"conclusion"`
	CompletedAt *time.Time `json:"completed_at"`
}

// pullRequestSnapshotRow scans one row of the snapshot query: every
// pull_requests field plus the four collections aggregated by the LATERAL
// joins. Callers see the domain PullRequestSnapshot from toSnapshot.
type pullRequestSnapshotRow struct {
	GithubID            int64      `db:"github_id"`
	Repo                string     `db:"repo"`
	PRNumber            int        `db:"pr_number"`
	Title               string     `db:"title"`
	Author              string     `db:"author"`
	Status              string     `db:"status"`
	Draft               bool       `db:"draft"`
	Additions           int        `db:"additions"`
	Deletions           int        `db:"deletions"`
	CommentsCount       int        `db:"comments_count"`
	HeadSHA             string     `db:"head_sha"`
	CreatedAt           time.Time  `db:"created_at"`
	MergedAt            *time.Time `db:"merged_at"`
	Reviews             []byte     `db:"reviews"`
	Labels              []string   `db:"labels"`
	ReviewRequests      []string   `db:"review_requests"`
	CheckRuns           []byte     `db:"check_runs"`
	ViewedAt            *time.Time `db:"viewed_at"`
	CommentsCountAtView *int       `db:"comments_count_at_view"`
	HeadSHAAtView       *string    `db:"head_sha_at_view"`
}

func (r pullRequestSnapshotRow) toSnapshot() (PullRequestSnapshot, error) {
	var reviewsJSON []reviewJSON
	if err := json.Unmarshal(r.Reviews, &reviewsJSON); err != nil {
		return PullRequestSnapshot{}, fmt.Errorf("decoding reviews: %w", err)
	}
	var checksJSON []checkRunJSON
	if err := json.Unmarshal(r.CheckRuns, &checksJSON); err != nil {
		return PullRequestSnapshot{}, fmt.Errorf("decoding check runs: %w", err)
	}

	reviews := make([]Review, len(reviewsJSON))
	for i, rev := range reviewsJSON {
		reviews[i] = Review(rev)
	}
	checks := make([]CheckRun, len(checksJSON))
	for i, c := range checksJSON {
		checks[i] = CheckRun(c)
	}

	var view *ViewState
	if r.ViewedAt != nil && r.CommentsCountAtView != nil && r.HeadSHAAtView != nil {
		view = &ViewState{
			ViewedAt:            *r.ViewedAt,
			CommentsCountAtView: *r.CommentsCountAtView,
			HeadSHAAtView:       *r.HeadSHAAtView,
		}
	}

	return PullRequestSnapshot{
		PullRequest: PullRequest{
			GithubID:  r.GithubID,
			Repo:      r.Repo,
			Number:    r.PRNumber,
			Title:     r.Title,
			Author:    r.Author,
			Status:    r.Status,
			Draft:     r.Draft,
			Additions: r.Additions,
			Deletions: r.Deletions,
			Comments:  r.CommentsCount,
			HeadSHA:   r.HeadSHA,
			CreatedAt: r.CreatedAt,
			MergedAt:  r.MergedAt,
		},
		Reviews:        reviews,
		Labels:         r.Labels,
		ReviewRequests: r.ReviewRequests,
		CheckRuns:      checks,
		View:           view,
	}, nil
}

// snapshotSelect is the SELECT body shared by ListSnapshotsForUser and
// GetSnapshotByNumber. The LATERAL joins collate each related collection
// into an array/jsonb per PR (no domain logic in SQL). The
// pull_request_views join is scoped to $1 (user_login) so each viewer sees
// their own read state; the poller's drift path passes an empty string,
// which never matches a real login, so view state comes back NULL.
//
// Callers append their own WHERE clause and bind values from $2.
const snapshotSelect = `
SELECT
    pr.github_id, pr.repo, pr.pr_number, pr.title, pr.author, pr.status, pr.draft,
    pr.additions, pr.deletions, pr.comments_count, pr.head_sha,
    pr.created_at, pr.merged_at,
    COALESCE(rev.list, '[]'::jsonb)         AS reviews,
    COALESCE(lab.list, ARRAY[]::TEXT[])     AS labels,
    COALESCE(req.list, ARRAY[]::TEXT[])     AS review_requests,
    COALESCE(chk.list, '[]'::jsonb)         AS check_runs,
    v.viewed_at                             AS viewed_at,
    v.comments_count_at_view                AS comments_count_at_view,
    v.head_sha_at_view                      AS head_sha_at_view
FROM pull_requests pr
LEFT JOIN LATERAL (
    SELECT jsonb_agg(jsonb_build_object(
        'reviewer',     r.reviewer,
        'verdict',      r.verdict,
        'submitted_at', r.submitted_at
    ) ORDER BY r.submitted_at) AS list
    FROM pull_request_reviews r
    WHERE r.pr_github_id = pr.github_id
) rev ON TRUE
LEFT JOIN LATERAL (
    SELECT array_agg(l.label ORDER BY l.label) AS list
    FROM pull_request_labels l
    WHERE l.pr_github_id = pr.github_id
) lab ON TRUE
LEFT JOIN LATERAL (
    SELECT array_agg(rr.reviewer ORDER BY rr.reviewer) AS list
    FROM pull_request_review_requests rr
    WHERE rr.pr_github_id = pr.github_id
) req ON TRUE
LEFT JOIN LATERAL (
    SELECT jsonb_agg(jsonb_build_object(
        'name',         c.check_name,
        'run_status',   c.run_status,
        'conclusion',   c.conclusion,
        'completed_at', c.completed_at
    ) ORDER BY c.check_name) AS list
    FROM pull_request_check_runs c
    WHERE c.repo = pr.repo AND c.head_sha = pr.head_sha
) chk ON TRUE
LEFT JOIN pull_request_views v ON v.pr_github_id = pr.github_id AND v.user_login = $1`

// ListSnapshotsForUser returns one snapshot per active PR (open or merged
// within recentlyMergedDays) in the repos the user observes, in a single atomic
// query. View state is scoped to userLogin so "unread" is computed against this
// user's own previous opens. The rules in rules.go then run on the result in Go.
// An empty repos slice matches nothing.
func (s *Store) ListSnapshotsForUser(ctx context.Context, userLogin string, repos []string, recentlyMergedDays int) ([]PullRequestSnapshot, error) {
	const query = snapshotSelect + `
WHERE pr.repo = ANY($3)
  AND (pr.status = 'open'
       OR (pr.status = 'merged' AND pr.merged_at > now() - make_interval(days => $2)))`

	rows, err := postgres.QueryMany(ctx, s.pool, query, []any{userLogin, recentlyMergedDays, repos}, pgx.RowToStructByName[pullRequestSnapshotRow])
	if err != nil {
		return nil, fmt.Errorf("listing pull request snapshots: %w", err)
	}
	out := make([]PullRequestSnapshot, len(rows))
	for i, r := range rows {
		snap, err := r.toSnapshot()
		if err != nil {
			return nil, err
		}
		out[i] = snap
	}
	return out, nil
}

// GetSnapshotByNumber returns the snapshot for one PR by (repo, pr_number);
// the bool is false when no row exists. The poller's drift path uses it to
// compare local check-run state against GitHub. View state is irrelevant
// here, so user_login is pinned to "" and the view columns come back NULL.
func (s *Store) GetSnapshotByNumber(ctx context.Context, repo string, number int) (PullRequestSnapshot, bool, error) {
	const query = snapshotSelect + `
WHERE pr.repo = $2 AND pr.pr_number = $3`

	row, err := postgres.QueryOne(ctx, s.pool, query, []any{"", repo, number}, pgx.RowToStructByName[pullRequestSnapshotRow])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PullRequestSnapshot{}, false, nil
		}
		return PullRequestSnapshot{}, false, fmt.Errorf("getting snapshot by number: %w", err)
	}
	snap, err := row.toSnapshot()
	if err != nil {
		return PullRequestSnapshot{}, false, err
	}
	return snap, true, nil
}
