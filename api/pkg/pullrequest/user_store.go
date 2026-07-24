package pullrequest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// Errors the profile store returns for the unique constraints, which the
// settings handler maps to a 409.
var (
	// ErrDuplicateCatchAll means the user already has an "all repositories"
	// profile (one catch-all per user).
	ErrDuplicateCatchAll = errors.New("an all-repositories profile already exists")
	// ErrRepoProfileConflict means a repo is already claimed by another specific
	// profile (one specific profile per repo).
	ErrRepoProfileConflict = errors.New("a repository is already in another profile")
	// ErrProfileNotFound means no profile with that id belongs to the user.
	ErrProfileNotFound = errors.New("profile not found")
)

type ruleProfileRow struct {
	ID                       int64    `db:"id"`
	Name                     string   `db:"name"`
	AllRepos                 bool     `db:"all_repos"`
	DefaultRequiredReviewers int      `db:"default_required_reviewers"`
	StaleAfterDays           int      `db:"stale_after_days"`
	RecentlyMergedDays       int      `db:"recently_merged_days"`
	IgnoreLabels             []string `db:"ignore_labels"`
	BotAuthors               []string `db:"bot_authors"`
}

type profileRepoRow struct {
	ProfileID int64  `db:"profile_id"`
	Repo      string `db:"repo"`
}

type profileOverrideRow struct {
	ProfileID int64  `db:"profile_id"`
	Label     string `db:"label"`
	Reviewers int    `db:"reviewers"`
}

// ListProfiles returns the user's rule profiles in display order, each with its
// targeted repos and reviewer overrides stitched on. It runs one query per
// child table so List can resolve every repo without a query per profile.
func (s *UserStore) ListProfiles(ctx context.Context, userLogin string) ([]RuleProfile, error) {
	rows, err := postgres.QueryMany(ctx, s.pool,
		`SELECT id, name, all_repos, default_required_reviewers, stale_after_days,
		        recently_merged_days, ignore_labels, bot_authors
		 FROM user_rule_profiles WHERE user_login = $1 ORDER BY position, id`,
		[]any{userLogin}, pgx.RowToStructByName[ruleProfileRow])
	if err != nil {
		return nil, fmt.Errorf("listing rule profiles: %w", err)
	}

	profiles := make([]RuleProfile, len(rows))
	byID := make(map[int64]*RuleProfile, len(rows))
	for i, r := range rows {
		profiles[i] = RuleProfile{
			ID:       r.ID,
			Name:     r.Name,
			AllRepos: r.AllRepos,
			Repos:    []string{},
			ReviewSettings: ReviewSettings{
				DefaultRequiredReviewers: r.DefaultRequiredReviewers,
				StaleAfterDays:           r.StaleAfterDays,
				RecentlyMergedDays:       r.RecentlyMergedDays,
				IgnoreLabels:             nonNil(r.IgnoreLabels),
				BotAuthors:               nonNil(r.BotAuthors),
				ReviewerOverrides:        []ReviewerOverride{},
			},
		}
		byID[r.ID] = &profiles[i]
	}

	repoRows, err := postgres.QueryMany(ctx, s.pool,
		`SELECT profile_id, repo FROM user_rule_profile_repos
		 WHERE user_login = $1 ORDER BY repo`,
		[]any{userLogin}, pgx.RowToStructByName[profileRepoRow])
	if err != nil {
		return nil, fmt.Errorf("listing profile repos: %w", err)
	}
	for _, rr := range repoRows {
		if p := byID[rr.ProfileID]; p != nil {
			p.Repos = append(p.Repos, rr.Repo)
		}
	}

	ovRows, err := postgres.QueryMany(ctx, s.pool,
		`SELECT o.profile_id, o.label, o.reviewers
		 FROM user_rule_profile_reviewer_overrides o
		 JOIN user_rule_profiles p ON p.id = o.profile_id
		 WHERE p.user_login = $1 ORDER BY o.label`,
		[]any{userLogin}, pgx.RowToStructByName[profileOverrideRow])
	if err != nil {
		return nil, fmt.Errorf("listing profile reviewer overrides: %w", err)
	}
	for _, o := range ovRows {
		if p := byID[o.ProfileID]; p != nil {
			p.ReviewerOverrides = append(p.ReviewerOverrides, ReviewerOverride{Label: o.Label, Reviewers: o.Reviewers})
		}
	}
	return profiles, nil
}

// CreateProfile inserts a new profile with its repos and overrides in one
// transaction and returns it with the assigned id. A duplicate catch-all or a
// repo already in another profile surfaces as ErrDuplicateCatchAll /
// ErrRepoProfileConflict.
func (s *UserStore) CreateProfile(ctx context.Context, userLogin string, p RuleProfile) (RuleProfile, error) {
	err := postgres.BeginTxFunc(ctx, s.pool, pgx.TxOptions{}, func(ctx context.Context) error {
		if err := postgres.QueryRow(ctx, s.pool, `
INSERT INTO user_rule_profiles (
    user_login, name, all_repos, default_required_reviewers, stale_after_days,
    recently_merged_days, ignore_labels, bot_authors, position, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    COALESCE((SELECT max(position) + 1 FROM user_rule_profiles WHERE user_login = $1), 0),
    now()
) RETURNING id`,
			[]any{userLogin, p.Name, p.AllRepos, p.DefaultRequiredReviewers, p.StaleAfterDays,
				p.RecentlyMergedDays, nonNil(p.IgnoreLabels), nonNil(p.BotAuthors)},
		).Scan(&p.ID); err != nil {
			return fmt.Errorf("inserting rule profile: %w", err)
		}
		return s.writeProfileChildren(ctx, userLogin, p)
	})
	if err != nil {
		return RuleProfile{}, mapProfileConflict(err)
	}
	return p, nil
}

// UpdateProfile replaces an existing profile's fields, repos, and overrides in
// one transaction. It returns ErrProfileNotFound if the id isn't the user's.
func (s *UserStore) UpdateProfile(ctx context.Context, userLogin string, p RuleProfile) error {
	err := postgres.BeginTxFunc(ctx, s.pool, pgx.TxOptions{}, func(ctx context.Context) error {
		tag, err := postgres.Exec(ctx, s.pool, `
UPDATE user_rule_profiles SET
    name                       = $3,
    all_repos                  = $4,
    default_required_reviewers = $5,
    stale_after_days           = $6,
    recently_merged_days       = $7,
    ignore_labels              = $8,
    bot_authors                = $9,
    updated_at                 = now()
WHERE id = $1 AND user_login = $2`,
			[]any{p.ID, userLogin, p.Name, p.AllRepos, p.DefaultRequiredReviewers,
				p.StaleAfterDays, p.RecentlyMergedDays, nonNil(p.IgnoreLabels), nonNil(p.BotAuthors)})
		if err != nil {
			return fmt.Errorf("updating rule profile: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrProfileNotFound
		}
		if _, err := postgres.Exec(ctx, s.pool,
			`DELETE FROM user_rule_profile_repos WHERE profile_id = $1`, []any{p.ID}); err != nil {
			return fmt.Errorf("clearing profile repos: %w", err)
		}
		if _, err := postgres.Exec(ctx, s.pool,
			`DELETE FROM user_rule_profile_reviewer_overrides WHERE profile_id = $1`, []any{p.ID}); err != nil {
			return fmt.Errorf("clearing profile reviewer overrides: %w", err)
		}
		return s.writeProfileChildren(ctx, userLogin, p)
	})
	if errors.Is(err, ErrProfileNotFound) {
		return err
	}
	return mapProfileConflict(err)
}

// DeleteProfile removes the user's profile (cascading to its children). It
// returns ErrProfileNotFound if the id isn't the user's.
func (s *UserStore) DeleteProfile(ctx context.Context, userLogin string, id int64) error {
	tag, err := postgres.Exec(ctx, s.pool,
		`DELETE FROM user_rule_profiles WHERE id = $1 AND user_login = $2`,
		[]any{id, userLogin})
	if err != nil {
		return fmt.Errorf("deleting rule profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProfileNotFound
	}
	return nil
}

// writeProfileChildren inserts a profile's repos and reviewer overrides. The
// caller is responsible for the surrounding transaction and for clearing any
// prior rows.
func (s *UserStore) writeProfileChildren(ctx context.Context, userLogin string, p RuleProfile) error {
	if !p.AllRepos {
		for _, repo := range p.Repos {
			if _, err := postgres.Exec(ctx, s.pool,
				`INSERT INTO user_rule_profile_repos (profile_id, user_login, repo) VALUES ($1, $2, $3)`,
				[]any{p.ID, userLogin, repo}); err != nil {
				return fmt.Errorf("inserting profile repo: %w", err)
			}
		}
	}
	for _, o := range p.ReviewerOverrides {
		if _, err := postgres.Exec(ctx, s.pool,
			`INSERT INTO user_rule_profile_reviewer_overrides (profile_id, label, reviewers) VALUES ($1, $2, $3)`,
			[]any{p.ID, o.Label, o.Reviewers}); err != nil {
			return fmt.Errorf("inserting profile reviewer override: %w", err)
		}
	}
	return nil
}

// mapProfileConflict translates the profile unique-constraint violations into
// the sentinel errors the handler maps to a 409, leaving other errors as-is.
func mapProfileConflict(err error) error {
	var pgErr *pgconn.PgError
	// 23505 is the SQLSTATE for unique_violation.
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return err
	}
	switch pgErr.ConstraintName {
	case "user_rule_profiles_all_repos_uniq":
		return ErrDuplicateCatchAll
	case "user_rule_profile_repos_user_login_repo_key":
		return ErrRepoProfileConflict
	default:
		return err
	}
}

// nonNil returns an empty slice for nil so JSON encodes "[]" and the Postgres
// text[] column gets '{}' rather than NULL.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
