package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	gh "github.com/google/go-github/v85/github"
	"github.com/rs/zerolog"

	"github.com/jspdown/dashboard/api/pkg/pullrequest"
)

// RepoConfig is a repo slug paired with its poll cadence.
type RepoConfig struct {
	Repo     string
	Interval time.Duration
}

// applier matches *Ingester so tests can stub it without a postgres pool.
type applier interface {
	Apply(ctx context.Context, event any) error
}

// repoSource yields the set of repos the poller should service: the union of
// every user's subscriptions.
type repoSource interface {
	DistinctRepos(ctx context.Context) ([]string, error)
}

// cursorStore persists per-repo sync cursors and poll health. The poller routes
// every repo_sync_cursors write through it so the store owns all Postgres
// access. *pullrequest.UserStore satisfies it.
type cursorStore interface {
	LoadCursor(ctx context.Context, repo string) (time.Time, error)
	SaveCursor(ctx context.Context, repo string, ts time.Time) error
	RecordPoll(ctx context.Context, repo string, lastError *string) error
}

// Poller pulls PR state from GitHub on a per-repo schedule and feeds it through
// Ingester. The repo set is dynamic: it reconciles against the union of users'
// subscriptions, starting a ticker for newly-observed repos and stopping it for
// repos no longer observed by anyone.
type Poller struct {
	client   *gh.Client
	ingester applier
	prs      staleSource
	repos    repoSource
	cursors  cursorStore
	interval time.Duration
	logger   zerolog.Logger

	// nudge requests an out-of-band reconcile so a just-added repo starts
	// polling without waiting for the next reconcile tick. Buffered so Nudge
	// never blocks; a pending nudge coalesces with the next reconcile.
	nudge chan struct{}
}

func NewPoller(
	client *gh.Client,
	ingester *Ingester,
	prs *pullrequest.Store,
	repos repoSource,
	cursors cursorStore,
	interval time.Duration,
	logger zerolog.Logger,
) *Poller {
	return &Poller{
		client:   client,
		ingester: ingester,
		prs:      prs,
		repos:    repos,
		cursors:  cursors,
		interval: interval,
		nudge:    make(chan struct{}, 1),
		logger:   logger.With().Str("component", "github_poller").Logger(),
	}
}

// authCheckInterval is how often watchAuth probes /rate_limit to confirm the
// token is still being honored.
const authCheckInterval = 5 * time.Minute

// reconcileInterval is how often Run re-reads the observed-repo union to start or
// stop per-repo pollers. A Nudge triggers one immediately between ticks.
const reconcileInterval = 30 * time.Second

// Run starts the auth health watcher and a reconcile loop that keeps one
// per-repo poller alive for each observed repo, blocking until ctx is done. The
// per-repo isolation (one ticker each) is preserved: a slow repo can't block a
// fast one.
func (p *Poller) Run(ctx context.Context) {
	p.logger.Info().Dur("interval", p.interval).Msg("Poller starting")

	var watch sync.WaitGroup
	watch.Go(func() { p.watchAuth(ctx) })

	var repoWG sync.WaitGroup
	active := make(map[string]context.CancelFunc)

	reconcile := func() {
		repos, err := p.repos.DistinctRepos(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				p.logger.Warn().Err(err).Msg("Reconcile failed, keeping current repo set")
			}
			return
		}

		desired := make(map[string]struct{}, len(repos))
		for _, repo := range repos {
			desired[repo] = struct{}{}
			if _, running := active[repo]; running {
				continue
			}

			rctx, cancel := context.WithCancel(ctx) //nolint:gosec // cancel is stored in active and invoked when the repo is dropped or on shutdown.
			active[repo] = cancel

			repoWG.Go(func() {
				p.runRepo(rctx, RepoConfig{Repo: repo, Interval: p.interval})
			})

			p.logger.Info().Str("repo", repo).Msg("Observing repo")
		}

		for repo, cancel := range active {
			if _, want := desired[repo]; !want {
				cancel()
				delete(active, repo)

				p.logger.Info().Str("repo", repo).Msg("Stopped observing repo")
			}
		}
	}

	reconcile()

	t := time.NewTicker(reconcileInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			for _, cancel := range active {
				cancel()
			}

			repoWG.Wait()
			watch.Wait()

			p.logger.Info().Msg("Poller stopped")

			return
		case <-t.C:
			reconcile()
		case <-p.nudge:
			reconcile()
		}
	}
}

// Nudge asks the reconcile loop to re-read the observed-repo union now. Safe to
// call from any goroutine and never blocks; if a reconcile is already pending
// the nudge is dropped (the pending one will pick up the change).
func (p *Poller) Nudge() {
	select {
	case p.nudge <- struct{}{}:
	default:
	}
}

// RunOnce runs a single poll tick for the named repo and returns its error.
// Behaves identically to a tick fired by Run, but doesn't require the repo to be
// in the active set: the e2e harness and the on-demand refresh endpoint drive a
// specific repo directly.
func (p *Poller) RunOnce(ctx context.Context, repoSlug string) error {
	return p.pollRepo(ctx, RepoConfig{Repo: repoSlug, Interval: p.interval})
}

func (p *Poller) runRepo(ctx context.Context, r RepoConfig) {
	logger := p.logger.With().Str("repo", r.Repo).Dur("interval", r.Interval).Logger()
	logger.Info().Msg("Repo poller starting")

	if err := p.pollRepo(ctx, r); err != nil && !errors.Is(err, context.Canceled) {
		logAPIError(logger, err, "Initial poll failed")
	}

	t := time.NewTicker(r.Interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Repo poller stopping")
			return
		case <-t.C:
			if err := p.pollRepo(ctx, r); err != nil && !errors.Is(err, context.Canceled) {
				logAPIError(logger, err, "Poll failed")
			}
		}
	}
}

// watchAuth periodically hits /rate_limit to catch the token being revoked,
// expired, or losing org SSO between poll ticks. The endpoint is free (doesn't
// count against the limit). A core limit of 60 means we're anonymous, the loud
// signal that the token needs rotating.
func (p *Poller) watchAuth(ctx context.Context) {
	p.checkAuth(ctx)

	t := time.NewTicker(authCheckInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.checkAuth(ctx)
		}
	}
}

func (p *Poller) checkAuth(ctx context.Context) {
	rl, _, err := p.client.RateLimit.Get(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		p.logger.Warn().Err(err).Msg("Rate limit probe failed")
		return
	}
	core := rl.GetCore()
	if core == nil {
		return
	}
	if core.Limit == 60 {
		p.logger.Error().
			Int("core_limit", core.Limit).
			Int("core_remaining", core.Remaining).
			Time("core_reset", core.Reset.Time).
			Str("auth_state", "anonymous").
			Msg("GitHub treating requests as anonymous, token likely revoked/expired or org SSO no longer authorized; rotate DASHBOARD_GITHUB_TOKEN")
		return
	}
	p.logger.Debug().
		Int("core_limit", core.Limit).
		Int("core_remaining", core.Remaining).
		Time("core_reset", core.Reset.Time).
		Str("auth_state", "authenticated").
		Msg("Auth health check")
}

// logAPIError logs a GitHub API error at a level keyed to its type: WARN for
// primary/secondary rate-limit errors (we recover on reset), ERROR otherwise.
// A limit of 60 is the anonymous ceiling, so we flag auth_state=anonymous: the
// token isn't being honored.
func logAPIError(logger zerolog.Logger, err error, baseMsg string) {
	var rl *gh.RateLimitError
	if errors.As(err, &rl) {
		if rl.Rate.Limit == 60 {
			logger.Error().
				Err(err).
				Int("rate_limit", rl.Rate.Limit).
				Int("rate_remaining", rl.Rate.Remaining).
				Time("rate_reset", rl.Rate.Reset.Time).
				Str("auth_state", "anonymous").
				Msg("GitHub treating requests as anonymous, token likely revoked/expired or org SSO no longer authorized; rotate DASHBOARD_GITHUB_TOKEN")
			return
		}
		logger.Warn().
			Err(err).
			Int("rate_limit", rl.Rate.Limit).
			Int("rate_remaining", rl.Rate.Remaining).
			Time("rate_reset", rl.Rate.Reset.Time).
			Msg(baseMsg)
		return
	}
	var arl *gh.AbuseRateLimitError
	if errors.As(err, &arl) {
		logger.Warn().Err(err).Str("kind", "secondary_rate_limit").Msg(baseMsg)
		return
	}
	var ghErr *gh.ErrorResponse
	if errors.As(err, &ghErr) && ghErr.Response != nil {
		logger.Error().Err(err).Int("status_code", ghErr.Response.StatusCode).Msg(baseMsg)
		return
	}
	logger.Error().Err(err).Msg(baseMsg)
}

// pollRepo runs a single tick for a repo: list updated PRs since the cursor,
// fan out to reviews and check runs, write through the ingester, advance the
// cursor.
func (p *Poller) pollRepo(ctx context.Context, r RepoConfig) (err error) {
	// Record the poll attempt and its outcome so the Repositories settings
	// screen can show per-repo health and a fresh "last sync" even when the
	// cursor doesn't advance (a tick with no new PRs).
	defer func() { p.recordPoll(ctx, r.Repo, err) }()

	owner, name, ok := strings.Cut(r.Repo, "/")
	if !ok {
		return fmt.Errorf("invalid repo %q, expected owner/name", r.Repo)
	}

	cursor, err := p.cursors.LoadCursor(ctx, r.Repo)
	if err != nil {
		return err
	}

	// On first run, restrict to open PRs to bound the backfill. Once we have a
	// cursor we widen to "all" so we catch state transitions (closed/merged).
	state := "all"
	if cursor.IsZero() {
		state = "open"
	}

	prs, newestSeen, err := p.listPullRequests(ctx, owner, name, state, cursor)
	if err != nil {
		return fmt.Errorf("listing pull requests: %w", err)
	}

	repoObj := repoFromSlug(r.Repo)
	applied := 0
	touched := make(map[int]struct{}, len(prs))
	for _, pr := range prs {
		if err := p.applyPullRequest(ctx, owner, name, repoObj, pr); err != nil {
			p.logger.Warn().Err(err).Str("repo", r.Repo).Int("number", pr.GetNumber()).Msg("Applying PR failed")
			continue
		}
		applied++
		touched[pr.GetNumber()] = struct{}{}
	}

	// Advance the cursor only if every PR in the batch ingested. Moving past a
	// failed PR would skip it forever: the next poll asks for "anything updated
	// since the cursor", and the failed PR is no longer in that window.
	if applied == len(prs) && !newestSeen.IsZero() {
		if err := p.cursors.SaveCursor(ctx, r.Repo, newestSeen); err != nil {
			return err
		}
	}

	// Drip-feed stale rows after the cursor batch. Anything the cursor caught
	// already has its version bumped, so it won't show up in the stale list.
	// Errors here are logged, never propagated.
	p.backfillStale(ctx, r, owner, name, repoObj)

	// Catch CI changes that don't bump pr.updated_at (e.g. a check run completing
	// on a quiet PR). One GraphQL call per tick spots divergent PRs, and we
	// re-fetch only those. PRs already touched above are skipped.
	p.refreshDriftedRollups(ctx, r, owner, name, repoObj, touched)

	p.logger.Debug().Str("repo", r.Repo).Int("changed_prs", len(prs)).Time("cursor", newestSeen).Msg("Poll complete")
	return nil
}

func (p *Poller) listPullRequests(ctx context.Context, owner, name, state string, cursor time.Time) ([]*gh.PullRequest, time.Time, error) {
	opts := &gh.PullRequestListOptions{
		State:       state,
		Sort:        "updated",
		Direction:   "desc",
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	var (
		all        []*gh.PullRequest
		newestSeen time.Time
	)

pages:
	for {
		page, resp, err := p.client.PullRequests.List(ctx, owner, name, opts)
		if err != nil {
			return nil, time.Time{}, err
		}

		for _, pr := range page {
			updated := pr.GetUpdatedAt().Time
			if updated.After(newestSeen) {
				newestSeen = updated
			}
			// Results are sorted updated desc, so once we cross the cursor
			// every remaining PR is older than what we already have.
			if !cursor.IsZero() && !updated.After(cursor) {
				break pages
			}
			all = append(all, pr)
		}

		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return all, newestSeen, nil
}

func (p *Poller) applyPullRequest(ctx context.Context, owner, name string, repo *gh.Repository, pr *gh.PullRequest) error {
	// The list endpoint returns the "pull-request-simple" shape, missing
	// additions/deletions/changed_files/mergeable. Re-fetch via Get so the diff
	// stats land in the snapshot.
	full, _, err := p.client.PullRequests.Get(ctx, owner, name, pr.GetNumber())
	if err != nil {
		return fmt.Errorf("fetching full pr: %w", err)
	}
	return p.applyFullPR(ctx, owner, name, repo, full)
}

// applyFullPR ingests an already-enriched PR plus its reviews and check runs.
// Shared by the cursor-batch path (applyPullRequest) and the startup stale
// backfill (refetchByNumber).
func (p *Poller) applyFullPR(ctx context.Context, owner, name string, repo *gh.Repository, pr *gh.PullRequest) error {
	prEvent := &gh.PullRequestEvent{PullRequest: pr, Repo: repo}
	if err := p.ingester.Apply(ctx, prEvent); err != nil {
		return fmt.Errorf("ingesting pr: %w", err)
	}

	if err := p.applyReviews(ctx, owner, name, repo, pr); err != nil {
		return fmt.Errorf("ingesting reviews: %w", err)
	}

	if err := p.applyCheckRuns(ctx, owner, name, repo, pr); err != nil {
		return fmt.Errorf("ingesting check runs: %w", err)
	}

	return nil
}

func (p *Poller) applyReviews(ctx context.Context, owner, name string, repo *gh.Repository, pr *gh.PullRequest) error {
	opts := &gh.ListOptions{PerPage: 100}
	for {
		reviews, resp, err := p.client.PullRequests.ListReviews(ctx, owner, name, pr.GetNumber(), opts)
		if err != nil {
			return err
		}

		for _, review := range reviews {
			event := &gh.PullRequestReviewEvent{
				PullRequest: pr,
				Review:      review,
				Repo:        repo,
			}
			if err := p.ingester.Apply(ctx, event); err != nil {
				p.logger.Warn().Err(err).Int64("review_id", review.GetID()).Int("number", pr.GetNumber()).Msg("Applying review failed")
			}
		}

		if resp == nil || resp.NextPage == 0 {
			return nil
		}
		opts.Page = resp.NextPage
	}
}

func (p *Poller) applyCheckRuns(ctx context.Context, owner, name string, repo *gh.Repository, pr *gh.PullRequest) error {
	headSHA := pr.GetHead().GetSHA()
	if headSHA == "" {
		return nil
	}

	opts := &gh.ListCheckRunsOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		runs, resp, err := p.client.Checks.ListCheckRunsForRef(ctx, owner, name, headSHA, opts)
		if err != nil {
			return err
		}
		if runs == nil {
			return nil
		}

		for _, cr := range runs.CheckRuns {
			event := &gh.CheckRunEvent{CheckRun: cr, Repo: repo}
			if err := p.ingester.Apply(ctx, event); err != nil {
				p.logger.Warn().Err(err).Int64("check_id", cr.GetID()).Str("head_sha", headSHA).Msg("Applying check run failed")
			}
		}

		if resp == nil || resp.NextPage == 0 {
			return nil
		}
		opts.Page = resp.NextPage
	}
}

// recordPoll persists the poll outcome on the repo's cursor row via the store.
// Best effort: a write failure is logged, never propagated, and a canceled
// context skips the write entirely. A nil pollErr clears a prior error.
func (p *Poller) recordPoll(ctx context.Context, repo string, pollErr error) {
	if p.cursors == nil || ctx.Err() != nil {
		return
	}
	var errText *string
	if pollErr != nil && !errors.Is(pollErr, context.Canceled) {
		s := pollErr.Error()
		errText = &s
	}
	if err := p.cursors.RecordPoll(ctx, repo, errText); err != nil {
		p.logger.Warn().Err(err).Str("repo", repo).Msg("Recording poll status failed")
	}
}

// repoFromSlug builds a *gh.Repository with the bits the ingester reads. We
// synthesize it because polled REST responses don't always carry the full
// Repository object that webhook payloads do.
func repoFromSlug(slug string) *gh.Repository {
	owner, name, _ := strings.Cut(slug, "/")
	return &gh.Repository{
		FullName: gh.Ptr(slug),
		Name:     gh.Ptr(name),
		Owner:    &gh.User{Login: gh.Ptr(owner)},
	}
}
