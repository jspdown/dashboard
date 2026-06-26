import { useState } from "react";

import Avatar from "../Avatar.jsx";
import Icon from "../Icon.jsx";

// Shared atoms for the per-user settings screens, ported from the design's
// settings-atoms. They render the design's global class names (see settings.css).

const HEALTH = {
  ok: { cls: "success", label: "polling" },
  checking: { cls: "warn", label: "checking" },
  error: { cls: "danger", label: "lost access" },
};

/** HealthDot shows a repo's polling status as a colored (optionally pulsing) dot. */
export function HealthDot({ status, withLabel }) {
  const m = HEALTH[status] || HEALTH.ok;
  return (
    <span className={"health " + m.cls} title={m.label}>
      <span className={"hdot " + m.cls + (status !== "error" ? " live" : "")} />
      {withLabel ? <span className="mono hlabel">{m.label}</span> : null}
    </span>
  );
}

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

/** SettingsShell renders the settings header, tab nav, and instant-save badge.
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
          <div className="set-saved mono"><span className="hdot success live" />changes apply instantly</div>
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
