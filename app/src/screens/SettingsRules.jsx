import { useEffect, useRef, useState } from "react";

import { getRepos, getRules, saveRules } from "../api/index.js";
import { useApi } from "../api/useApi.js";
import Icon from "../components/Icon.jsx";
import { ChipInput, SettingsShell, Stepper } from "../components/settings/atoms.jsx";

// Bounds mirror the server-side validation (see pkg/pullrequest).
const MAX_REVIEWERS = 10;
const MAX_STALE_DAYS = 30;
const MAX_MERGED_DAYS = 60;

const SAVE_DEBOUNCE_MS = 400;

// Override rows need stable React keys while their labels are edited (and may be
// blank or duplicated mid-edit), so each carries a client-only _id, stripped
// before saving.
let overrideSeq = 0;
const nextOverrideId = () => ++overrideSeq;

export default function SettingsRules() {
  const { data: loaded } = useApi(getRules);
  const { data: reposData } = useApi(getRepos);
  const [rules, setRules] = useState(null);
  const [status, setStatus] = useState("saved"); // saved | saving | error
  const timer = useRef(null);

  useEffect(() => {
    if (loaded && !rules) {
      setRules({
        ...loaded,
        reviewer_overrides: (loaded.reviewer_overrides ?? []).map((o) => ({ ...o, _id: nextOverrideId() })),
      });
    }
  }, [loaded, rules]);
  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);

  const repoCount = (reposData ?? []).length;

  // persist saves the whole rules object after a short debounce, dropping any
  // half-typed (blank-label) overrides so they don't trip server validation.
  function persist(next) {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      setStatus("saving");
      const payload = {
        ...next,
        reviewer_overrides: next.reviewer_overrides
          .filter((o) => o.label.trim() !== "")
          .map((o) => ({ label: o.label, reviewers: o.reviewers })),
      };
      saveRules(payload).then(() => setStatus("saved")).catch(() => setStatus("error"));
    }, SAVE_DEBOUNCE_MS);
  }

  function update(patch) {
    setRules((prev) => {
      const next = { ...prev, ...patch };
      persist(next);
      return next;
    });
  }

  if (!rules) {
    return <SettingsShell tab="rules" ready={false}><div className="set-empty mono">Loading your review rules…</div></SettingsShell>;
  }

  const overrides = rules.reviewer_overrides ?? [];
  const setOverride = (i, patch) =>
    update({ reviewer_overrides: overrides.map((o, idx) => (idx === i ? { ...o, ...patch } : o)) });

  return (
    <SettingsShell tab="rules">
      <div className="rules-note ai-block">
        These defaults apply to every repository you observe. Changes take effect across all your groups on the next refresh, no re-sync.
      </div>

      <div className="rule-card">
        <div className="rule-head">
          <Icon name="users" size={13} />
          <h3>Required reviewers</h3>
          <span className="rule-sub">how many approvals a PR needs before it leaves <span className="mono">Needs my review</span></span>
        </div>
        <div className="rule-body">
          <div className="rule-line">
            <div className="rl-label">Default for all repos</div>
            <Stepper value={rules.default_required_reviewers} onChange={(v) => update({ default_required_reviewers: v })} min={0} max={MAX_REVIEWERS} />
          </div>
          <div className="rule-divider" />
          <div className="rule-sublabel mono">per-label overrides</div>
          <div className="label-rules">
            {overrides.map((lr, i) => (
              <div className="label-rule" key={lr._id}>
                <span className="ci-chip label">
                  <Icon name="tag" size={10} />
                  <input
                    className="ci-field mono"
                    style={{ minWidth: 100 }}
                    value={lr.label}
                    placeholder="label"
                    onChange={(e) => setOverride(i, { label: e.target.value })}
                  />
                </span>
                <span className="lr-arrow mono">needs</span>
                <Stepper value={lr.reviewers} onChange={(v) => setOverride(i, { reviewers: v })} min={0} max={MAX_REVIEWERS} />
                <button
                  type="button"
                  className="btn ghost lr-x"
                  onClick={() => update({ reviewer_overrides: overrides.filter((_, idx) => idx !== i) })}
                  aria-label="remove override"
                >
                  <Icon name="x" size={11} />
                </button>
              </div>
            ))}
            <button
              type="button"
              className="btn ghost lr-add"
              onClick={() => update({ reviewer_overrides: [...overrides, { label: "", reviewers: 1, _id: nextOverrideId() }] })}
            >
              <Icon name="plus" size={11} />Add label override
            </button>
          </div>
        </div>
      </div>

      <div className="rule-card">
        <div className="rule-head">
          <Icon name="tag" size={13} />
          <h3>Ignore labels</h3>
          <span className="rule-sub">PRs carrying any of these never enter your review queue</span>
        </div>
        <div className="rule-body">
          <ChipInput
            chips={rules.ignore_labels ?? []}
            onAdd={(c) => update({ ignore_labels: [...new Set([...(rules.ignore_labels ?? []), c])] })}
            onRemove={(c) => update({ ignore_labels: (rules.ignore_labels ?? []).filter((i) => i !== c) })}
            placeholder="add label and press enter"
            tone="label"
          />
        </div>
      </div>

      <div className="rule-card">
        <div className="rule-head">
          <Icon name="git-branch" size={13} />
          <h3>Bot authors</h3>
          <span className="rule-sub">authors grouped under <span className="mono">Renovate / bots</span> instead of the main queue</span>
        </div>
        <div className="rule-body">
          <ChipInput
            chips={rules.bot_authors ?? []}
            onAdd={(c) => update({ bot_authors: [...new Set([...(rules.bot_authors ?? []), c])] })}
            onRemove={(c) => update({ bot_authors: (rules.bot_authors ?? []).filter((i) => i !== c) })}
            placeholder="add bot login and press enter"
            tone="bot"
          />
        </div>
      </div>

      <div className="rule-card">
        <div className="rule-head">
          <Icon name="clock" size={13} />
          <h3>Freshness windows</h3>
          <span className="rule-sub">when a PR looks stale, and how far back recently merged reaches</span>
        </div>
        <div className="rule-body">
          <div className="rule-line">
            <div className="rl-label">
              Mark <span className="mono">stale</span> after
              <div className="rl-help">adds the warm-to-red badge on the PR row</div>
            </div>
            <Stepper value={rules.stale_after_days} onChange={(v) => update({ stale_after_days: v })} min={1} max={MAX_STALE_DAYS} suffix="d" />
          </div>
          <div className="rule-divider" />
          <div className="rule-line">
            <div className="rl-label">
              <span className="mono">Recently merged</span> looks back
              <div className="rl-help">how long merged PRs stay in that group</div>
            </div>
            <Stepper value={rules.recently_merged_days} onChange={(v) => update({ recently_merged_days: v })} min={1} max={MAX_MERGED_DAYS} suffix="d" />
          </div>
        </div>
      </div>

      <div className="rules-foot mono">
        {status === "saving" && <><span className="hdot warn live" />saving…</>}
        {status === "saved" && <><span className="hdot success live" />all changes saved · applied to <span className="hl">{repoCount} observed {repoCount === 1 ? "repo" : "repos"}</span></>}
        {status === "error" && <><span className="hdot danger" />save failed · your last change will retry when you edit again</>}
      </div>
    </SettingsShell>
  );
}
