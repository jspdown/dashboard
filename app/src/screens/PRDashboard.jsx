import { useState, useMemo, useRef, useEffect, useCallback } from "react";

import styles from "./PRDashboard.module.css";
import { useAuth } from "../api/authContext.js";
import { useConfig } from "../api/configContext.js";
import { getPRs, markPRViewed } from "../api/index.js";
import { useApi } from "../api/useApi.js";
import Avatar from "../components/Avatar.jsx";
import CIBadge from "../components/CIBadge.jsx";
import Icon from "../components/Icon.jsx";
import ReviewBadge from "../components/ReviewBadge.jsx";
import StaleBadge from "../components/StaleBadge.jsx";
import { stalenessBg, stalenessLabel } from "../components/staleness.js";

// buildGroups returns the group definitions, weaving descriptions from
// runtime config. Only "review" and "merged" vary with config; the rest
// are fixed.
function buildGroups(config) {
  return [
    { id: "mine",     label: "My open PRs",       icon: "pr",    headClass: "groupMine",     description: "Open PRs you authored." },
    { id: "reviewed", label: "Reviewed by me",    icon: "check", headClass: "groupReviewed", description: "Open PRs you've already reviewed and aren't currently re-requested on." },
    { id: "review",   label: "Needs my review",   icon: "alert", headClass: "groupReview",   description: reviewGroupDescription(config.review) },
    { id: "renovate", label: "Renovate",          icon: "rerun", headClass: "groupRenovate", description: "Renovate dependency PRs awaiting review, separated from the main queue." },
    { id: "merged",   label: "Recently merged",   icon: "merge", headClass: "groupMerged",   description: `PRs merged in the last ${config.recently_merged_days} days.` },
  ];
}

// reviewGroupDescription builds the "Needs my review" tooltip from the
// configured review policy, so the labels here match what the backend applies.
function reviewGroupDescription(review) {
  const sentences = ["Open non-draft PRs that still need your review."];
  const clauses = [];
  for (const label of review.ignore_labels ?? []) {
    clauses.push(`${label} is skipped`);
  }
  for (const o of review.reviewer_overrides ?? []) {
    const noun = o.reviewers === 1 ? "reviewer" : "reviewers";
    clauses.push(`${o.label} needs ${o.reviewers} ${noun} instead of ${review.default_required_reviewers}`);
  }
  if (clauses.length > 0) sentences.push(clauses.join("; ") + ".");
  return sentences.join(" ");
}

const COLLAPSE_STORAGE_KEY = "dashboard:collapsedGroups";
const DEFAULT_COLLAPSED_GROUPS = ["reviewed"];

function loadCollapsedGroups() {
  try {
    const raw = localStorage.getItem(COLLAPSE_STORAGE_KEY);
    return new Set(raw ? JSON.parse(raw) : DEFAULT_COLLAPSED_GROUPS);
  } catch {
    return new Set(DEFAULT_COLLAPSED_GROUPS);
  }
}

// Groups that show the "new since you last viewed" indicator. The others
// ignore the unread/new_activity fields.
const UNREAD_GROUPS = new Set(["mine", "reviewed"]);

// Verdicts the viewer can have left on a PR in "Reviewed by me". Anything
// else (commented, dismissed, missing) is neutral: no tint, sorted last.
const VERDICT_BLOCKED = "changes_requested";
const VERDICT_APPROVED = "approved";

// reviewedSortKey: blocked first (easy to spot), then approved, then the rest.
function reviewedSortKey(pr) {
  if (pr.viewer_verdict === VERDICT_BLOCKED) return 0;
  if (pr.viewer_verdict === VERDICT_APPROVED) return 1;
  return 2;
}

// reviewedRowBg tints a "Reviewed by me" row by the viewer's verdict
// instead of the PR's age.
function reviewedRowBg(verdict) {
  if (verdict === VERDICT_BLOCKED) return "var(--state-danger-dim)";
  if (verdict === VERDICT_APPROVED) return "var(--state-success-dim)";
  return "transparent";
}

// activityTooltip builds the dot's title text. "Never viewed" when there's
// no baseline, otherwise "+N commit/comment/review" parts joined with " · ".
function activityTooltip(newActivity) {
  if (!newActivity) return "Never viewed";
  const parts = [];
  if (newActivity.new_commits) parts.push("+1 commit");
  if (newActivity.new_comments > 0) {
    parts.push(`+${newActivity.new_comments} comment${newActivity.new_comments === 1 ? "" : "s"}`);
  }
  if (newActivity.new_reviews > 0) {
    parts.push(`+${newActivity.new_reviews} review${newActivity.new_reviews === 1 ? "" : "s"}`);
  }
  return parts.length > 0 ? parts.join(" · ") : "New activity";
}

// Parses a query like "repo:auth ci:failing author:alex needs work" into a
// predicate. Prefixed tokens narrow by field; bare tokens match the title.
function buildQueryMatcher(query) {
  const tokens = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (tokens.length === 0) return () => true;
  const fieldOf = { repo: "repo", author: "author", ci: "ci" };
  const checks = tokens.map(tok => {
    const colon = tok.indexOf(":");
    if (colon > 0) {
      const field = fieldOf[tok.slice(0, colon)];
      const value = tok.slice(colon + 1);
      if (field && value) return pr => String(pr[field] ?? "").toLowerCase().includes(value);
    }
    return pr => String(pr.title ?? "").toLowerCase().includes(tok);
  });
  return pr => checks.every(fn => fn(pr));
}

function PRRow({ pr, group, staleAfterDays, selected, rowRef, isUnread, onMarkViewed, onSelect }) {
  const stale = pr.age >= staleAfterDays;
  const isMerged = group === "merged";
  const isReviewed = group === "reviewed";
  function activate() {
    onSelect();
    if (isUnread) onMarkViewed(pr);
    window.open(`https://github.com/${pr.repo}/pull/${pr.num}`, "_blank", "noopener");
  }
  function handleMarkSeen(e) {
    e.stopPropagation();
    onMarkViewed(pr);
  }
  let background;
  if (isMerged) background = "transparent";
  else if (isReviewed) background = reviewedRowBg(pr.viewer_verdict);
  else background = stalenessBg(pr.age);
  return (
    <div
      ref={rowRef}
      className={[styles.prRow, stale ? styles.stale : "", isMerged ? styles.merged : "", selected ? styles.selected : "", isUnread ? styles.unreadRow : ""].join(" ")}
      data-pr-id={pr.id}
      data-pr-num={pr.num}
      data-pr-repo={pr.repo}
      data-pr-group={group}
      style={{ background }}
      role="button"
      tabIndex={0}
      onClick={activate}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          activate();
        }
      }}
    >
      <div className={styles.prState}>
        {isMerged
          ? <span className={`${styles.prGlyph} ${styles.mergedGlyph}`}><Icon name="merge" size={14} /></span>
          : <span className={`${styles.prGlyph} ${group === "review" ? styles.reviewGlyph : styles.openGlyph}`}><Icon name="pr" size={14} /></span>
        }
      </div>
      <div className={styles.prTitle}>
        <div className={styles.prTitleLine}>
          {isUnread ? (
            <span
              className={styles.unreadDot}
              title={activityTooltip(pr.new_activity)}
              aria-label={`Unread: ${activityTooltip(pr.new_activity)}`}
            />
          ) : null}
          <span className={styles.tText}>{pr.title}</span>
          {!isMerged && <StaleBadge age={pr.age} threshold={staleAfterDays} />}
        </div>
        <div className={`${styles.prTitleMeta} mono`}>
          <span className={styles.repo}>{pr.repo}</span>
          <span className={styles.hash}>#{pr.num}</span>
          <span className={styles.diff}>{pr.changes}</span>
          {pr.comments > 0 && <span className={styles.cmts}><Icon name="comment" size={12} />{pr.comments}</span>}
        </div>
      </div>
      <div className={styles.prFootRow}>
        <div className={styles.prAuthor}>
          <Avatar name={pr.author} size={20} />
          <span className="mono">{pr.author}</span>
        </div>
        <div className={styles.prCi}>
          <CIBadge status={pr.ci} />
          <span className={`${styles.checks} mono`}>{pr.checks}</span>
        </div>
        <div className={`${styles.prReview} mono`}>
          <ReviewBadge approvals={pr.approvals} required={pr.required_approvals} state={pr.merge_state} />
        </div>
        <div className={`${styles.prAge} mono`}>
          {pr.merged
            ? <span className={styles.mergedWhen}>{pr.merged}</span>
            : <span>{stalenessLabel(pr.age)}</span>
          }
        </div>
        <div className={styles.prBlock}>
          {pr.blocking.length > 0 ? (
            <div className={styles.blockStack}>
              {pr.blocking.slice(0, 3).map(b => <Avatar key={b} name={b} size={18} />)}
            </div>
          ) : <span className={`mono ${styles.dash}`}>—</span>}
        </div>
      </div>
      <div className={styles.prActions}>
        {isUnread ? (
          <button
            type="button"
            className={`btn ghost ${styles.markSeenBtn}`}
            onClick={handleMarkSeen}
            title="Mark as seen"
            aria-label="Mark as seen"
          >
            <Icon name="check" size={12} />
          </button>
        ) : null}
        <button className="btn ghost" tabIndex={-1}><Icon name="chevron-right" size={12} /></button>
      </div>
    </div>
  );
}

export default function PRDashboard() {
  const config = useConfig();
  const auth = useAuth();
  const groups = useMemo(() => buildGroups(config), [config]);
  const staleAfterDays = config.stale_after_days;

  const [filter, setFilter] = useState("all");
  const [sort, setSort]     = useState("priority");
  const [query, setQuery]   = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [collapsedGroups, setCollapsedGroups] = useState(loadCollapsedGroups);
  // viewedThisSession optimistically clears the unread dot before the next
  // refetch. Reverted on POST failure so a row doesn't look read when the
  // server still thinks it's unread.
  const [viewedThisSession, setViewedThisSession] = useState(() => new Set());

  const markViewed = useCallback((pr) => {
    setViewedThisSession(prev => {
      if (prev.has(pr.id)) return prev;
      const next = new Set(prev);
      next.add(pr.id);
      return next;
    });
    markPRViewed(pr.id).catch(err => {
      // Mark failed: drop the optimistic entry so the dot reappears for a retry.
      console.warn("markPRViewed failed", err);
      setViewedThisSession(prev => {
        if (!prev.has(pr.id)) return prev;
        const next = new Set(prev);
        next.delete(pr.id);
        return next;
      });
    });
  }, []);

  const isUnread = useCallback(
    (pr) => UNREAD_GROUPS.has(pr.group) && pr.unread && !viewedThisSession.has(pr.id),
    [viewedThisSession],
  );

  useEffect(() => {
    try {
      localStorage.setItem(COLLAPSE_STORAGE_KEY, JSON.stringify([...collapsedGroups]));
    } catch { /* localStorage unavailable: fall back to in-memory state */ }
  }, [collapsedGroups]);

  function toggleGroup(id) {
    setCollapsedGroups(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  }

  const searchRef = useRef(null);
  const rowRefs = useRef([]);

  const { data: prs = [], loading: prsLoading } = useApi(() => getPRs({ filter, sort }), [filter, sort]);

  const visiblePRs = useMemo(() => {
    const match = buildQueryMatcher(query);
    return prs.filter(match);
  }, [prs, query]);

  // Per-group ordered slices. "reviewed" is sorted blocked/approved/rest;
  // other groups keep the API order.
  const groupedPRs = useMemo(() => {
    const out = {};
    groups.forEach(g => {
      const rows = visiblePRs.filter(p => p.group === g.id);
      if (g.id === "reviewed") {
        rows.sort((a, b) => reviewedSortKey(a) - reviewedSortKey(b));
      }
      out[g.id] = rows;
    });
    return out;
  }, [visiblePRs, groups]);

  const counts = useMemo(() => {
    const c = {};
    groups.forEach(g => { c[g.id] = groupedPRs[g.id].length; });
    return c;
  }, [groupedPRs, groups]);

  const reviewedCounts = useMemo(() => {
    const rows = groupedPRs.reviewed;
    let blocked = 0;
    let approved = 0;
    for (const pr of rows) {
      if (pr.viewer_verdict === VERDICT_BLOCKED) blocked++;
      else if (pr.viewer_verdict === VERDICT_APPROVED) approved++;
    }
    return { blocked, approved };
  }, [groupedPRs]);

  // Flat render-order list for j/k navigation. Collapsed groups are skipped
  // so navigation never lands on a hidden row.
  const orderedPRs = useMemo(
    () => groups.flatMap(g => collapsedGroups.has(g.id) ? [] : groupedPRs[g.id]),
    [groupedPRs, collapsedGroups, groups],
  );

  useEffect(() => {
    if (selectedIndex >= orderedPRs.length) setSelectedIndex(Math.max(0, orderedPRs.length - 1));
  }, [orderedPRs.length, selectedIndex]);

  useEffect(() => {
    function onKeyDown(e) {
      const target = e.target;
      const typing = target instanceof HTMLElement
        && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable);

      if (e.key === "/" && !typing) {
        e.preventDefault();
        searchRef.current?.focus();
        return;
      }
      if (typing) {
        if (e.key === "Escape") target.blur();
        return;
      }
      if (e.metaKey || e.ctrlKey || e.altKey) return;

      if (e.key === "j" || e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIndex(i => Math.min(i + 1, Math.max(0, orderedPRs.length - 1)));
      } else if (e.key === "k" || e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIndex(i => Math.max(i - 1, 0));
      } else if (e.key === "Enter") {
        const pr = orderedPRs[selectedIndex];
        if (pr) {
          if (isUnread(pr)) markViewed(pr);
          window.open(`https://github.com/${pr.repo}/pull/${pr.num}`, "_blank", "noopener");
        }
      } else if (e.key === "r") {
        const pr = orderedPRs[selectedIndex];
        if (pr) {
          if (isUnread(pr)) markViewed(pr);
          window.open(`https://github.com/${pr.repo}/pull/${pr.num}/files`, "_blank", "noopener");
        }
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [orderedPRs, selectedIndex, isUnread, markViewed]);

  useEffect(() => {
    rowRefs.current[selectedIndex]?.scrollIntoView({ block: "nearest" });
  }, [selectedIndex]);

  return (
    <div className={styles.prdash}>
      <header className={styles.prHead}>
        <div className={styles.prHeadTop}>
          <h1>Pull requests</h1>
          <div className={`${styles.prSummary} mono`}>
            <span><span className="dot info" /> {counts.mine} open</span>
            <span><span className="dot accent" /> {counts.review} need you</span>
            <span className={styles.muted}>{counts.merged} merged</span>
          </div>
        </div>
        <div className={styles.prToolbar}>
          <div className={styles.prSearch}>
            <Icon name="search" size={12} />
            <input
              ref={searchRef}
              value={query}
              onChange={e => setQuery(e.target.value)}
              placeholder={`filter… repo:auth ci:failing author:${auth.login}`}
            />
            <span className="kbd">/</span>
          </div>
          <div className={styles.prChips}>
            {["all", "needs review", `stale > ${staleAfterDays}d`, "ci failing"].map(c => (
              <button key={c} className={["chip", filter === c ? "active" : ""].join(" ")} onClick={() => setFilter(c)}>{c}</button>
            ))}
          </div>
          <div className={styles.prSort}>
            <Icon name="sort" size={12} />
            <select value={sort} onChange={e => setSort(e.target.value)}>
              <option value="priority">priority</option>
              <option value="age">age</option>
              <option value="repo">repo</option>
              <option value="author">author</option>
            </select>
          </div>
        </div>
      </header>

      <div className={styles.prTable} data-prs-loaded={prsLoading ? "false" : "true"}>
        <div className={`${styles.prTableHead} mono`}>
          <div className={styles.thState} />
          <div className={styles.thTitle}>title</div>
          <div className={styles.thAuthor}>author</div>
          <div className={styles.thChecks}>checks</div>
          <div className={styles.thReview}>review</div>
          <div className={styles.thAge}>age</div>
          <div className={styles.thBlock}>blocking</div>
          <div className={styles.thAct} />
        </div>

        {groups.map(g => {
          const isCollapsed = collapsedGroups.has(g.id);
          return (
            <div key={g.id} className={styles.prGroup} data-group-id={g.id}>
              <button
                type="button"
                className={[styles.prGroupHead, styles[g.headClass]].join(" ")}
                onClick={() => toggleGroup(g.id)}
                aria-expanded={!isCollapsed}
                data-group-head={g.id}
              >
                <div className={styles.gTitle}>
                  <Icon name={isCollapsed ? "chevron-right" : "chevron-down"} size={12} />
                  <Icon name={g.icon} size={12} />
                  <span title={g.description}>{g.label}</span>
                  <span className={`mono ${styles.gCount}`}>{counts[g.id]}</span>
                  {g.id === "reviewed" && counts[g.id] > 0 && (
                    <span className={`mono ${styles.gSplit}`}>
                      <span className={styles.gBlocked} title="blocked by you">
                        <span className="dot danger" /> {reviewedCounts.blocked} blocked
                      </span>
                      <span className={styles.gApproved} title="approved by you">
                        <span className="dot success" /> {reviewedCounts.approved} approved
                      </span>
                    </span>
                  )}
                </div>
                {g.id === "review" && counts[g.id] > 0 && !isCollapsed && <span className={`${styles.gHint} mono`}>priority order ↓</span>}
              </button>
              {!isCollapsed && groupedPRs[g.id].map(pr => {
                const idx = orderedPRs.indexOf(pr);
                return (
                  <PRRow
                    key={pr.id}
                    pr={pr}
                    group={g.id}
                    staleAfterDays={staleAfterDays}
                    selected={idx === selectedIndex}
                    rowRef={el => { rowRefs.current[idx] = el; }}
                    isUnread={isUnread(pr)}
                    onMarkViewed={markViewed}
                    onSelect={() => setSelectedIndex(idx)}
                  />
                );
              })}
            </div>
          );
        })}
      </div>

      <footer className={`${styles.prFoot} mono`}>
        <span>prs/</span>
        <span className={styles.footDot} />
        <span>last sync 12 sec ago</span>
        <span className={styles.spacer} />
        <span className={styles.footHints}>
          <span><span className="kbd">j</span> <span className="kbd">k</span> navigate</span>
          <span><span className="kbd">⏎</span> open</span>
          <span><span className="kbd">r</span> review</span>
        </span>
      </footer>
    </div>
  );
}
