package pullrequest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jspdown/dashboard/api/pkg/postgres"
)

// ErrRepoInaccessible reports that the server PAT can't reach a repo (it doesn't
// exist or the token lacks access). The repo verifier returns it and the
// settings handler maps it to a 422 so the add-repo UI can show a clear message.
var ErrRepoInaccessible = errors.New("repository not found or token lacks access")

// UserStore persists per-user repositories and review rules: the data behind the
// Repositories and Review rules settings screens. It's separate from Store
// (which holds shared PR data) because this state is scoped to a viewer.
type UserStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) *UserStore {
	return &UserStore{pool: pool}
}

// ListRepos returns the repos a user observes, sorted.
func (s *UserStore) ListRepos(ctx context.Context, userLogin string) ([]string, error) {
	repos, err := postgres.QueryMany(ctx, s.pool,
		`SELECT repo FROM user_repos WHERE user_login = $1 ORDER BY repo`,
		[]any{userLogin}, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("listing user repos: %w", err)
	}
	return repos, nil
}

// DistinctRepos returns the union of every user's observed repos. The poller
// services exactly this set, so a repo keeps polling as long as anyone observes
// it and stops once the last subscriber removes it.
func (s *UserStore) DistinctRepos(ctx context.Context) ([]string, error) {
	repos, err := postgres.QueryMany(ctx, s.pool,
		`SELECT DISTINCT repo FROM user_repos ORDER BY repo`,
		nil, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("listing distinct repos: %w", err)
	}
	return repos, nil
}

// HasRepo reports whether the user already observes repo.
func (s *UserStore) HasRepo(ctx context.Context, userLogin, repo string) (bool, error) {
	var exists bool
	err := postgres.QueryRow(ctx, s.pool,
		`SELECT EXISTS(SELECT 1 FROM user_repos WHERE user_login = $1 AND repo = $2)`,
		[]any{userLogin, repo}).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking user repo: %w", err)
	}
	return exists, nil
}

// AddRepo records that the user observes repo. Idempotent.
func (s *UserStore) AddRepo(ctx context.Context, userLogin, repo string) error {
	if _, err := postgres.Exec(ctx, s.pool,
		`INSERT INTO user_repos (user_login, repo) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		[]any{userLogin, repo}); err != nil {
		return fmt.Errorf("adding user repo: %w", err)
	}
	return nil
}

// RemoveRepo drops the user's subscription to repo. The shared PR rows stay so
// teammates still observing it keep their data; this only hides it for the user.
func (s *UserStore) RemoveRepo(ctx context.Context, userLogin, repo string) error {
	if _, err := postgres.Exec(ctx, s.pool,
		`DELETE FROM user_repos WHERE user_login = $1 AND repo = $2`,
		[]any{userLogin, repo}); err != nil {
		return fmt.Errorf("removing user repo: %w", err)
	}
	return nil
}

// RepoSuggestion is a repo observed by teammates that the user hasn't added,
// with the logins of those who observe it.
type RepoSuggestion struct {
	Repo      string   `db:"repo"      json:"repo"`
	Observers []string `db:"observers" json:"observers"`
}

// suggestionLimit caps how many team suggestions the settings screen shows.
const suggestionLimit = 8

// Suggestions returns repos the user's teammates observe but the user doesn't,
// most-observed first, so the settings screen can offer one-click adds.
func (s *UserStore) Suggestions(ctx context.Context, userLogin string) ([]RepoSuggestion, error) {
	const query = `
SELECT repo, array_agg(user_login ORDER BY user_login) AS observers
FROM user_repos
WHERE repo NOT IN (SELECT repo FROM user_repos WHERE user_login = $1)
GROUP BY repo
ORDER BY count(*) DESC, repo
LIMIT $2`

	suggs, err := postgres.QueryMany(ctx, s.pool, query,
		[]any{userLogin, suggestionLimit}, pgx.RowToStructByName[RepoSuggestion])
	if err != nil {
		return nil, fmt.Errorf("listing repo suggestions: %w", err)
	}
	return suggs, nil
}

// RepoStatus is a repo's polling health, read from repo_sync_cursors.
type RepoStatus struct {
	Repo         string     `db:"repo"`
	LastSyncedAt *time.Time `db:"last_synced_at"`
	LastPolledAt *time.Time `db:"last_polled_at"`
	LastError    *string    `db:"last_error"`
}

// RepoStatuses returns the polling status for each of repos, keyed by slug.
// Repos with no cursor row yet (just added, never polled) are absent from the
// map, which the caller reads as "checking".
func (s *UserStore) RepoStatuses(ctx context.Context, repos []string) (map[string]RepoStatus, error) {
	rows, err := postgres.QueryMany(ctx, s.pool,
		`SELECT repo, last_synced_at, last_polled_at, last_error
		 FROM repo_sync_cursors WHERE repo = ANY($1)`,
		[]any{repos}, pgx.RowToStructByName[RepoStatus])
	if err != nil {
		return nil, fmt.Errorf("listing repo statuses: %w", err)
	}
	out := make(map[string]RepoStatus, len(rows))
	for _, r := range rows {
		out[r.Repo] = r
	}
	return out, nil
}

// LoadCursor returns the repo's sync cursor: the updated_at high-water mark from
// the last successful poll. A repo with no cursor row yet returns the zero time.
func (s *UserStore) LoadCursor(ctx context.Context, repo string) (time.Time, error) {
	var ts *time.Time
	err := postgres.QueryRow(ctx, s.pool,
		`SELECT last_synced_at FROM repo_sync_cursors WHERE repo = $1`,
		[]any{repo}).Scan(&ts)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("loading cursor: %w", err)
	}
	if ts == nil {
		return time.Time{}, nil
	}
	return *ts, nil
}

// SaveCursor advances the repo's sync cursor to ts. It touches only
// last_synced_at, leaving the health columns to RecordPoll.
func (s *UserStore) SaveCursor(ctx context.Context, repo string, ts time.Time) error {
	if _, err := postgres.Exec(ctx, s.pool, `
INSERT INTO repo_sync_cursors (repo, last_synced_at) VALUES ($1, $2)
ON CONFLICT (repo) DO UPDATE SET last_synced_at = EXCLUDED.last_synced_at`,
		[]any{repo, ts}); err != nil {
		return fmt.Errorf("saving cursor: %w", err)
	}
	return nil
}

// RecordPoll stamps the last poll attempt time and its outcome on the repo's
// cursor row, touching only the health columns and leaving the cursor
// (last_synced_at) to SaveCursor. A nil lastError clears a prior error.
func (s *UserStore) RecordPoll(ctx context.Context, repo string, lastError *string) error {
	if _, err := postgres.Exec(ctx, s.pool, `
INSERT INTO repo_sync_cursors (repo, last_polled_at, last_error) VALUES ($1, now(), $2)
ON CONFLICT (repo) DO UPDATE SET last_polled_at = now(), last_error = EXCLUDED.last_error`,
		[]any{repo, lastError}); err != nil {
		return fmt.Errorf("recording poll status: %w", err)
	}
	return nil
}

type userSettingsRow struct {
	DefaultRequiredReviewers int      `db:"default_required_reviewers"`
	StaleAfterDays           int      `db:"stale_after_days"`
	RecentlyMergedDays       int      `db:"recently_merged_days"`
	IgnoreLabels             []string `db:"ignore_labels"`
	BotAuthors               []string `db:"bot_authors"`
}

type reviewerOverrideRow struct {
	Label     string `db:"label"`
	Reviewers int    `db:"reviewers"`
}

// GetSettings returns the user's review rules, falling back to the built-in
// defaults when they've never saved any. Reviewer overrides are loaded
// separately and attached.
func (s *UserStore) GetSettings(ctx context.Context, userLogin string) (UserSettings, error) {
	settings := DefaultUserSettings()

	row, err := postgres.QueryOne(ctx, s.pool,
		`SELECT default_required_reviewers, stale_after_days, recently_merged_days,
		        ignore_labels, bot_authors
		 FROM user_settings WHERE user_login = $1`,
		[]any{userLogin}, pgx.RowToStructByName[userSettingsRow])
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Keep defaults; a user may still have overrides without a settings row
		// in theory, so fall through to load them.
	case err != nil:
		return UserSettings{}, fmt.Errorf("getting user settings: %w", err)
	default:
		settings.DefaultRequiredReviewers = row.DefaultRequiredReviewers
		settings.StaleAfterDays = row.StaleAfterDays
		settings.RecentlyMergedDays = row.RecentlyMergedDays
		settings.IgnoreLabels = nonNil(row.IgnoreLabels)
		settings.BotAuthors = nonNil(row.BotAuthors)
	}

	overrides, err := postgres.QueryMany(ctx, s.pool,
		`SELECT label, reviewers FROM user_reviewer_overrides WHERE user_login = $1 ORDER BY label`,
		[]any{userLogin}, pgx.RowToStructByName[reviewerOverrideRow])
	if err != nil {
		return UserSettings{}, fmt.Errorf("getting reviewer overrides: %w", err)
	}
	settings.ReviewerOverrides = make([]ReviewerOverride, len(overrides))
	for i, o := range overrides {
		settings.ReviewerOverrides[i] = ReviewerOverride(o)
	}
	return settings, nil
}

// SaveSettings replaces the user's review rules wholesale in one transaction:
// the scalar/array settings row plus the full set of reviewer overrides.
func (s *UserStore) SaveSettings(ctx context.Context, userLogin string, us UserSettings) error {
	return postgres.BeginTxFunc(ctx, s.pool, pgx.TxOptions{}, func(ctx context.Context) error {
		if _, err := postgres.Exec(ctx, s.pool, `
INSERT INTO user_settings (
    user_login, default_required_reviewers, stale_after_days, recently_merged_days,
    ignore_labels, bot_authors, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT (user_login) DO UPDATE SET
    default_required_reviewers = EXCLUDED.default_required_reviewers,
    stale_after_days           = EXCLUDED.stale_after_days,
    recently_merged_days       = EXCLUDED.recently_merged_days,
    ignore_labels              = EXCLUDED.ignore_labels,
    bot_authors                = EXCLUDED.bot_authors,
    updated_at                 = now()`,
			[]any{userLogin, us.DefaultRequiredReviewers, us.StaleAfterDays, us.RecentlyMergedDays,
				nonNil(us.IgnoreLabels), nonNil(us.BotAuthors)}); err != nil {
			return fmt.Errorf("upserting user settings: %w", err)
		}

		if _, err := postgres.Exec(ctx, s.pool,
			`DELETE FROM user_reviewer_overrides WHERE user_login = $1`,
			[]any{userLogin}); err != nil {
			return fmt.Errorf("clearing reviewer overrides: %w", err)
		}
		for _, o := range us.ReviewerOverrides {
			if _, err := postgres.Exec(ctx, s.pool,
				`INSERT INTO user_reviewer_overrides (user_login, label, reviewers) VALUES ($1, $2, $3)
				 ON CONFLICT (user_login, label) DO UPDATE SET reviewers = EXCLUDED.reviewers`,
				[]any{userLogin, o.Label, o.Reviewers}); err != nil {
				return fmt.Errorf("inserting reviewer override: %w", err)
			}
		}
		return nil
	})
}

// nonNil returns an empty slice for nil so JSON encodes "[]" and the Postgres
// text[] column gets '{}' rather than NULL.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
