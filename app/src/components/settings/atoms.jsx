import { useState } from "react";

import Avatar from "../Avatar.jsx";
import Icon from "../Icon.jsx";
import { nextOverrideId } from "./overrides.js";

// Shared atoms for the settings screens. They render global class names (see
// settings.css).

/** Stepper is a numeric +/- control, clamped to [min, max]. */
export function Stepper({ value, onChange, min = 0, max = 99, suffix }) {
  return (
    <div className="stepper">
      <button type="button" className="stp-btn" onClick={() => onChange(Math.max(min, value - 1))} aria-label="decrease">
        <Icon name="minus" size={12} />
      </button>
      <span className="stp-val mono">{value}{suffix ? <span className="stp-suffix">{suffix}</span> : null}</span>
      <button type="button" className="stp-btn" onClick={() => onChange(Math.min(max, value + 1))} aria-label="increase">
        <Icon name="plus" size={12} />
      </button>
    </div>
  );
}

/** ChipInput edits a list of labels/logins: type and press Enter to add. */
export function ChipInput({ chips, onAdd, onRemove, placeholder, tone = "label" }) {
  const [draft, setDraft] = useState("");
  const commit = () => {
    const v = draft.trim();
    if (v) { onAdd(v); setDraft(""); }
  };
  return (
    <div className="chip-input">
      {chips.map((c) => (
        <span key={c} className={"ci-chip " + tone}>
          <Icon name={tone === "bot" ? "git-branch" : "tag"} size={10} />
          <span className="mono">{c}</span>
          <button type="button" className="ci-x" onClick={() => onRemove(c)} aria-label={"remove " + c}>
            <Icon name="x" size={10} />
          </button>
        </span>
      ))}
      <input
        className="ci-field mono"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") { e.preventDefault(); commit(); }
          if (e.key === "Backspace" && !draft && chips.length) onRemove(chips[chips.length - 1]);
        }}
        placeholder={placeholder}
      />
    </div>
  );
}

/** ReviewerOverridesEditor edits a list of per-label required-reviewer counts.
 * `overrides` rows carry a client-only `_id`; `onChange` receives the next list. */
export function ReviewerOverridesEditor({ overrides, onChange, max = 10 }) {
  const setRow = (i, patch) => onChange(overrides.map((o, idx) => (idx === i ? { ...o, ...patch } : o)));
  return (
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
              onChange={(e) => setRow(i, { label: e.target.value })}
            />
          </span>
          <span className="lr-arrow mono">needs</span>
          <Stepper value={lr.reviewers} onChange={(v) => setRow(i, { reviewers: v })} min={0} max={max} />
          <button
            type="button"
            className="btn ghost lr-x"
            onClick={() => onChange(overrides.filter((_, idx) => idx !== i))}
            aria-label="remove override"
          >
            <Icon name="x" size={11} />
          </button>
        </div>
      ))}
      <button
        type="button"
        className="btn ghost lr-add"
        onClick={() => onChange([...overrides, { label: "", reviewers: 1, _id: nextOverrideId() }])}
      >
        <Icon name="plus" size={11} />Add label override
      </button>
    </div>
  );
}

/** RepoScopePicker selects which repos a profile targets. "All repositories"
 * makes it the catch-all; otherwise each observed repo is a toggle. A repo
 * already claimed by another profile is disabled, naming the owner. */
export function RepoScopePicker({ allRepos, selected, options = [], onToggleAll, onToggleRepo }) {
  return (
    <div className="repo-scope">
      <label className="scope-all">
        <input type="checkbox" checked={allRepos} onChange={(e) => onToggleAll(e.target.checked)} />
        <span>All repositories</span>
        <span className="scope-all-hint mono">every observed repo no other profile claims</span>
      </label>
      {allRepos ? null : (
        <div className="scope-repos">
          {options.length === 0
            ? <span className="scope-empty mono">Add repositories on the Repositories tab first.</span>
            : options.map((o) => {
              const checked = selected.includes(o.repo);
              const locked = !checked && o.takenBy;
              return (
                <label key={o.repo} className={"scope-repo" + (locked ? " locked" : "")} title={locked ? `Already in "${o.takenBy}"` : undefined}>
                  <input type="checkbox" checked={checked} disabled={locked} onChange={() => onToggleRepo(o.repo)} />
                  <Icon name="github" size={12} />
                  <span className="mono">{o.repo}</span>
                  {locked ? <span className="scope-taken mono">in {o.takenBy}</span> : null}
                </label>
              );
            })}
        </div>
      )}
    </div>
  );
}

/** RepoSuggestion is a team-observed repo the viewer can add in one click. */
export function RepoSuggestion({ repo, observers = [], onAdd, adding }) {
  return (
    <div className="repo-sugg">
      <div className="rs-left">
        <Icon name="github" size={13} />
        <span className="rs-name mono">{repo}</span>
        <span className="rs-obs">
          <span className="rs-avatars">
            {observers.slice(0, 3).map((o) => <Avatar key={o} name={o} size={16} />)}
          </span>
          <span className="mono rs-count">{observers.length} observing</span>
        </span>
      </div>
      <button type="button" className="btn rs-add" onClick={() => onAdd(repo)} disabled={adding}>
        <Icon name="plus" size={11} />{adding ? "Adding" : "Add"}
      </button>
    </div>
  );
}

const TABS = [
  { id: "repos", label: "Repositories", icon: "folder", href: "#/settings/repos" },
  { id: "rules", label: "Review rules", icon: "sliders", href: "#/settings/rules" },
];

/** SettingsShell renders the settings header and tab nav.
 * `ready` flips data-settings-ready once the screen's data has loaded, which the
 * e2e/screenshot harness waits on (mirrors the dashboard's data-prs-loaded). */
export function SettingsShell({ tab, ready = true, children }) {
  return (
    <div className="settings" data-settings-ready={ready ? "true" : "false"}>
      <header className="set-head">
        <div className="set-head-top">
          <div className="set-title">
            <Icon name="settings" size={15} />
            <h1>Settings</h1>
          </div>
        </div>
        <nav className="set-tabs">
          {TABS.map((t) => (
            <a key={t.id} className={"set-tab" + (tab === t.id ? " active" : "")} href={t.href}>
              <Icon name={t.icon} size={13} />{t.label}
            </a>
          ))}
        </nav>
      </header>
      <div className="set-body">{children}</div>
    </div>
  );
}
