package github

import (
	"context"
	"errors"
	"fmt"

	gh "github.com/google/go-github/v85/github"

	"github.com/jspdown/dashboard/api/pkg/pullrequest"
)

// staleBudgetPerTick caps how many stale PRs each repo backfills per tick. The
// cap stops a version bump from bursting GitHub quota right after a deploy, and
// a degraded GitHub can't stall catch-up since failed numbers retry next tick.
const staleBudgetPerTick = 5

// staleSource is the slice of pullrequest.Store the backfill paths need, so
// tests can inject a fake without standing up postgres.
type staleSource interface {
	ListStaleIngestNumbers(ctx context.Context, repo string, currentVersion int) ([]int, error)
	GetSnapshotByNumber(ctx context.Context, repo string, number int) (pullrequest.PullRequestSnapshot, bool, error)
}

// refetchByNumber fetches a PR by number and ingests it through the same
// post-Get path the cursor batch uses.
func (p *Poller) refetchByNumber(ctx context.Context, owner, name string, repo *gh.Repository, number int) error {
	full, _, err := p.client.PullRequests.Get(ctx, owner, name, number)
	if err != nil {
		return fmt.Errorf("fetching full pr: %w", err)
	}
	return p.applyFullPR(ctx, owner, name, repo, full)
}

// backfillStale re-ingests up to staleBudgetPerTick active PRs whose stored
// ingest_version is below IngestVersion. pollRepo calls it at the end of each
// tick so a version bump drip-feeds without bursting quota. Failures are
// logged and swallowed.
func (p *Poller) backfillStale(ctx context.Context, r RepoConfig, owner, name string, repoObj *gh.Repository) {
	logger := p.logger.With().Str("repo", r.Repo).Logger()

	numbers, err := p.prs.ListStaleIngestNumbers(ctx, r.Repo, IngestVersion)
	if err != nil {
		logger.Warn().Err(err).Msg("Listing stale ingest numbers failed; skipping backfill")
		return
	}
	if len(numbers) == 0 {
		logger.Debug().Int("ingest_version", IngestVersion).Msg("No stale prs to backfill")
		return
	}

	budget := min(len(numbers), staleBudgetPerTick)

	var refetched, failed int
	for _, n := range numbers[:budget] {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := p.refetchByNumber(ctx, owner, name, repoObj, n); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			logAPIError(logger.With().Int("number", n).Logger(), err, "Stale backfill failed")
			failed++
			continue
		}
		refetched++
	}
	logger.Info().
		Int("ingest_version", IngestVersion).
		Int("stale_refetched", refetched).
		Int("stale_failed", failed).
		Int("stale_remaining", len(numbers)-budget).
		Msg("Stale backfill tick")
}

// refreshDriftedRollups asks GitHub (GraphQL statusCheckRollup) for the CI
// state of every open PR and re-ingests any whose local snapshot disagrees.
// This is the backstop for changes GitHub doesn't surface on pr.updated_at,
// mainly check-run completions on a quiet PR. Numbers in skip were already
// touched by the cursor batch this tick, so we exclude them.
//
// The GraphQL rollup combines check runs and commit statuses, but ours is
// check runs only, so a repo on legacy commit statuses always looks divergent.
// We detect that (db has no check runs, GitHub reports a non-empty rollup) and
// skip the refresh, since refetchByNumber pulls only check runs and the next
// tick would diverge again. Logged as status_only so it's visible without churn.
func (p *Poller) refreshDriftedRollups(ctx context.Context, r RepoConfig, owner, name string, repoObj *gh.Repository, skip map[int]struct{}) {
	logger := p.logger.With().Str("repo", r.Repo).Logger()

	rollups, err := p.listCheckRollups(ctx, owner, name)
	if err != nil {
		logAPIError(logger, err, "Rollup drift fetch failed")
		return
	}

	var refreshed, failed, checked, statusOnly int
	for _, rl := range rollups {
		if err := ctx.Err(); err != nil {
			return
		}
		if _, dup := skip[rl.Number]; dup {
			continue
		}
		checked++

		snap, found, err := p.prs.GetSnapshotByNumber(ctx, r.Repo, rl.Number)
		if err != nil {
			logger.Warn().Err(err).Int("number", rl.Number).Msg("Rollup drift snapshot lookup failed")
			failed++
			continue
		}

		want := mapRollup(rl.RollupState)
		var have string
		var dbHeadSHA string
		if found {
			have, _, _ = pullrequest.RollupCI(snap.CheckRuns)
			dbHeadSHA = snap.HeadSHA
		} else {
			have = pullrequest.CINone
		}

		shaMismatch := found && rl.HeadRefOID != "" && rl.HeadRefOID != dbHeadSHA
		stateMismatch := want != have

		if !found || shaMismatch || stateMismatch {
			// Status-only repo guard: DB has no check runs for the current head,
			// GitHub reports a non-empty rollup, and the SHA matches.
			// refetchByNumber can't fix it (we don't ingest commit statuses), so
			// log once per tick instead of thrashing the API.
			if found && !shaMismatch && len(snap.CheckRuns) == 0 && rl.RollupState != "" && rl.RollupState != "PENDING" && rl.RollupState != "EXPECTED" {
				statusOnly++
				logger.Debug().
					Int("number", rl.Number).
					Str("rollup_state", rl.RollupState).
					Msg("Rollup divergence ignored: repo appears to use legacy commit statuses (not ingested)")
				continue
			}

			if err := p.refetchByNumber(ctx, owner, name, repoObj, rl.Number); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				logAPIError(logger.With().Int("number", rl.Number).Logger(), err, "Rollup drift refresh failed")
				failed++
				continue
			}
			refreshed++
		}
	}

	logger.Info().
		Int("rollup_checked", checked).
		Int("rollup_refreshed", refreshed).
		Int("rollup_failed", failed).
		Int("rollup_status_only", statusOnly).
		Msg("Rollup drift refresh tick")
}
