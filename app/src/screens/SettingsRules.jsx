import { useEffect, useRef, useState } from "react";

import { createProfile, deleteProfile, getProfiles, getRepos, updateProfile } from "../api/index.js";
import { useApi } from "../api/useApi.js";
import Icon from "../components/Icon.jsx";
import { ChipInput, RepoScopePicker, ReviewerOverridesEditor, SettingsShell, Stepper } from "../components/settings/atoms.jsx";
import { stripOverrideIds, withOverrideIds } from "../components/settings/overrides.js";

// Bounds mirror the server-side validation (see pkg/pullrequest).
const MAX_REVIEWERS = 10;
const MAX_STALE_DAYS = 30;
const MAX_MERGED_DAYS = 60;

const SAVE_DEBOUNCE_MS = 400;

function newProfileDraft() {
  return {
    name: "New profile",
    all_repos: false,
    repos: [],
    default_required_reviewers: 2,
    stale_after_days: 5,
    recently_merged_days: 7,
    ignore_labels: [],
    bot_authors: [],
    reviewer_overrides: [],
  };
}

// ProfileCard edits one profile and auto-saves on a short debounce. It keeps its
// own local copy (seeded once from props) so a parent refetch doesn't clobber an
// in-flight edit; repoOptions still flow in live to keep "taken" repos disabled.
function ProfileCard({ profile, repoOptions, onSaved, onDelete }) {
  const [p, setP] = useState(() => ({ ...profile, reviewer_overrides: withOverrideIds(profile.reviewer_overrides) }));
  const [status, setStatus] = useState("saved"); // saved | saving | error
  const [error, setError] = useState("");
  const timer = useRef(null);

  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);

  function persist(next) {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      setStatus("saving");
      const payload = {
        name: next.name,
        all_repos: next.all_repos,
        repos: next.all_repos ? [] : next.repos,
        default_required_reviewers: next.default_required_reviewers,
        stale_after_days: next.stale_after_days,
        recently_merged_days: next.recently_merged_days,
        ignore_labels: next.ignore_labels,
        bot_authors: next.bot_authors,
        reviewer_overrides: stripOverrideIds(next.reviewer_overrides),
      };
      updateProfile(profile.id, payload)
        .then(() => { setStatus("saved"); setError(""); onSaved?.(); return null; })
        .catch((err) => { setStatus("error"); setError(err.message || "save failed"); });
    }, SAVE_DEBOUNCE_MS);
  }

  function update(patch) {
    setP((prev) => {
      const next = { ...prev, ...patch };
      persist(next);
      return next;
    });
  }

  function toggleRepo(repo) {
    update({ repos: p.repos.includes(repo) ? p.repos.filter((r) => r !== repo) : [...p.repos, repo] });
  }

  return (
    <div className="profile-card">
      <div className="profile-head">
        <Icon name="sliders" size={13} />
        <input
          className="profile-name"
          value={p.name}
          onChange={(e) => update({ name: e.target.value })}
          placeholder="Profile name"
          aria-label="profile name"
        />
        <button type="button" className="btn ghost profile-del" onClick={() => onDelete(profile.id)} aria-label={"delete " + p.name}>
          <Icon name="x" size={12} />Delete
        </button>
      </div>

      <div className="profile-body">
        <div className="rule-sublabel mono">Applies to</div>
        <RepoScopePicker
          allRepos={p.all_repos}
          selected={p.repos}
          options={repoOptions}
          onToggleAll={(v) => update({ all_repos: v })}
          onToggleRepo={toggleRepo}
        />

        <div className="rule-divider" />
        <div className="rule-line">
          <div className="rl-label">Required reviewers</div>
          <Stepper value={p.default_required_reviewers} onChange={(v) => update({ default_required_reviewers: v })} min={0} max={MAX_REVIEWERS} />
        </div>
        <div className="rule-sublabel mono">per-label overrides</div>
        <ReviewerOverridesEditor
          overrides={p.reviewer_overrides}
          onChange={(next) => update({ reviewer_overrides: next })}
          max={MAX_REVIEWERS}
        />

        <div className="rule-divider" />
        <div className="rule-sublabel mono">Ignore labels</div>
        <ChipInput
          chips={p.ignore_labels}
          onAdd={(c) => update({ ignore_labels: [...new Set([...p.ignore_labels, c])] })}
          onRemove={(c) => update({ ignore_labels: p.ignore_labels.filter((i) => i !== c) })}
          placeholder="add label and press enter"
          tone="label"
        />

        <div className="rule-sublabel mono">Bot authors</div>
        <ChipInput
          chips={p.bot_authors}
          onAdd={(c) => update({ bot_authors: [...new Set([...p.bot_authors, c])] })}
          onRemove={(c) => update({ bot_authors: p.bot_authors.filter((i) => i !== c) })}
          placeholder="add bot login and press enter"
          tone="bot"
        />

        <div className="rule-divider" />
        <div className="rule-line">
          <div className="rl-label">Mark <span className="mono">stale</span> after</div>
          <Stepper value={p.stale_after_days} onChange={(v) => update({ stale_after_days: v })} min={1} max={MAX_STALE_DAYS} suffix="d" />
        </div>
        <div className="rule-line">
          <div className="rl-label"><span className="mono">Recently merged</span> looks back</div>
          <Stepper value={p.recently_merged_days} onChange={(v) => update({ recently_merged_days: v })} min={1} max={MAX_MERGED_DAYS} suffix="d" />
        </div>
      </div>

      <div className="profile-foot mono">
        {status === "saving" && <><span className="hdot warn live" />saving…</>}
        {status === "saved" && <><span className="hdot success live" />saved</>}
        {status === "error" && <><span className="hdot danger" />{error}</>}
      </div>
    </div>
  );
}

export default function SettingsRules() {
  const { data: loaded } = useApi(getProfiles);
  const { data: reposData } = useApi(getRepos);
  const [profiles, setProfiles] = useState(null);
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    if (loaded && profiles === null) setProfiles(loaded);
  }, [loaded, profiles]);

  function refetch() {
    getProfiles().then(setProfiles).catch(() => {});
  }

  async function create() {
    setCreating(true);
    try {
      const made = await createProfile(newProfileDraft());
      setProfiles((prev) => [...(prev ?? []), made]);
    } finally {
      setCreating(false);
    }
  }

  async function remove(id) {
    await deleteProfile(id);
    setProfiles((prev) => (prev ?? []).filter((p) => p.id !== id));
  }

  // repoOptionsFor lists the observed repos for a profile's picker, flagging any
  // repo already claimed by a different specific profile so it can be disabled.
  function repoOptionsFor(selfId) {
    const owned = new Map();
    for (const p of profiles ?? []) {
      if (p.id === selfId || p.all_repos) continue;
      for (const repo of p.repos ?? []) owned.set(repo, p.name);
    }
    return (reposData ?? []).map((r) => ({ repo: r.repo, takenBy: owned.get(r.repo) }));
  }

  if (!profiles) {
    return <SettingsShell tab="rules" ready={false}><div className="set-empty mono">Loading your review rules…</div></SettingsShell>;
  }

  return (
    <SettingsShell tab="rules">
      <div className="rules-note ai-block">
        Review rules live in profiles. Each profile applies to a set of repositories, or to all of them. A repository
        picks the specific profile that lists it, otherwise the all-repositories profile, otherwise the built-in defaults.
      </div>

      {profiles.length === 0 ? (
        <div className="set-empty mono">
          <span>No rule profiles yet.</span>
          <span>Create one to set required reviewers, ignored labels, bots, and freshness windows.</span>
        </div>
      ) : (
        <div className="profile-list">
          {profiles.map((p) => (
            <ProfileCard key={p.id} profile={p} repoOptions={repoOptionsFor(p.id)} onSaved={refetch} onDelete={remove} />
          ))}
        </div>
      )}

      <button type="button" className="btn primary profile-add" onClick={create} disabled={creating}>
        <Icon name="plus" size={13} />{creating ? "Creating…" : "New profile"}
      </button>
    </SettingsShell>
  );
}
