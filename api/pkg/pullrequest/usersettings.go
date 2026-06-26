package pullrequest

// Built-in defaults applied to a user who hasn't customized their review rules.
const (
	DefaultRequiredReviewers  = 2
	DefaultStaleAfterDays     = 5
	DefaultRecentlyMergedDays = 7

	// MinDays / MaxDays bound the freshness windows a user can configure. The
	// stale-ingest backfill (see store.go) uses MaxRecentlyMergedDays so a row
	// stays eligible for any user's widest possible window.
	MinDays               = 1
	MaxStaleAfterDays     = 30
	MaxRecentlyMergedDays = 60

	// MaxRequiredReviewers caps a configurable reviewer count.
	MaxRequiredReviewers = 10
)

// ReviewerOverride pairs a PR label with a non-default required-reviewer count.
type ReviewerOverride struct {
	Label     string `json:"label"`
	Reviewers int    `json:"reviewers"`
}

// UserSettings is one user's review-rule defaults: the knobs the Review rules
// settings screen edits. They drive classification (Rules) and the freshness
// windows the store and views read.
type UserSettings struct {
	DefaultRequiredReviewers int
	StaleAfterDays           int
	RecentlyMergedDays       int
	IgnoreLabels             []string
	BotAuthors               []string
	ReviewerOverrides        []ReviewerOverride
}

// DefaultUserSettings returns the built-in rules for a user with no saved row.
func DefaultUserSettings() UserSettings {
	return UserSettings{
		DefaultRequiredReviewers: DefaultRequiredReviewers,
		StaleAfterDays:           DefaultStaleAfterDays,
		RecentlyMergedDays:       DefaultRecentlyMergedDays,
		IgnoreLabels:             []string{},
		BotAuthors:               []string{},
		ReviewerOverrides:        []ReviewerOverride{},
	}
}
