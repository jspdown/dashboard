package pullrequest

import "slices"

// Built-in defaults applied to a repo no rule profile covers.
const (
	DefaultRequiredReviewers  = 2
	DefaultStaleAfterDays     = 5
	DefaultRecentlyMergedDays = 7

	// MinDays / MaxDays bound the freshness windows a profile can configure. The
	// stale-ingest backfill (see store.go) uses MaxRecentlyMergedDays so a row
	// stays eligible for any profile's widest possible window.
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

// ReviewSettings is the review policy applied to one repo's PRs: the knobs a
// rule profile carries. It drives classification (Rules), the required-reviewer
// count, and the freshness windows the store and views read.
type ReviewSettings struct {
	DefaultRequiredReviewers int
	StaleAfterDays           int
	RecentlyMergedDays       int
	IgnoreLabels             []string
	BotAuthors               []string
	ReviewerOverrides        []ReviewerOverride
}

// DefaultReviewSettings returns the built-in policy for a repo no profile covers.
func DefaultReviewSettings() ReviewSettings {
	return ReviewSettings{
		DefaultRequiredReviewers: DefaultRequiredReviewers,
		StaleAfterDays:           DefaultStaleAfterDays,
		RecentlyMergedDays:       DefaultRecentlyMergedDays,
		IgnoreLabels:             []string{},
		BotAuthors:               []string{},
		ReviewerOverrides:        []ReviewerOverride{},
	}
}

// RuleProfile is a named, self-contained review policy scoped to a set of repos.
// A profile either targets an explicit repo list or, when AllRepos is set, every
// repo the viewer observes that no specific profile claims. Profiles never
// inherit from one another; see ResolveProfile for how a repo picks one.
type RuleProfile struct {
	ID       int64
	Name     string
	AllRepos bool
	Repos    []string
	ReviewSettings
}

// matchProfile returns the profile that applies to repo: the specific profile
// that lists it, else the all-repos catch-all, else nil. Specific profiles win
// outright; there is no merging.
func matchProfile(profiles []RuleProfile, repo string) *RuleProfile {
	var catchAll *RuleProfile
	for i := range profiles {
		p := &profiles[i]
		if p.AllRepos {
			catchAll = p
			continue
		}
		if slices.Contains(p.Repos, repo) {
			return p
		}
	}
	return catchAll
}

// ResolveProfile returns the review policy that applies to repo: the matched
// profile's settings, or the built-in defaults when no profile covers it.
func ResolveProfile(profiles []RuleProfile, repo string) ReviewSettings {
	if p := matchProfile(profiles, repo); p != nil {
		return p.ReviewSettings
	}
	return DefaultReviewSettings()
}

// MaxRecentlyMergedWindow returns the widest recently-merged window across the
// profiles, falling back to the default for repos no profile covers. List uses
// it to fetch one snapshot set wide enough for every repo, then narrows each
// repo to its own profile's window.
func MaxRecentlyMergedWindow(profiles []RuleProfile) int {
	// A repo no profile covers falls back to the default window, so floor at it.
	widest := DefaultRecentlyMergedDays
	for _, p := range profiles {
		if p.RecentlyMergedDays > widest {
			widest = p.RecentlyMergedDays
		}
	}
	return widest
}
