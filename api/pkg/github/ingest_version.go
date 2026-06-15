package github

// IngestVersion identifies the schema-shape this ingester writes. Bump when
// the pipeline starts populating a new per-PR field or related table that
// pre-existing rows wouldn't have. The poller's per-tick stale backfill (see
// backfill.go) re-fetches active PRs whose stored version is below this
// value, so a bump backfills affected rows automatically without a manual
// cursor reset.
//
// History:
//
//	1 - adds pull_request_labels (now part of the squashed 20260510120000_init migration)
//	2 - rewrites pull_request_reviews.verdict after fixing case-insensitive
//	    parsing of GitHub's review state (no migration; data correction only)
const IngestVersion = 2
