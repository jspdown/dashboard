import { useState } from "react";

import {
  addRepo,
  getRepos,
  getRepoSuggestions,
  recheckRepo,
  removeRepo,
} from "../api/index.js";
import { useApi } from "../api/useApi.js";
import Icon from "../components/Icon.jsx";
import { RepoSuggestion, SettingsShell } from "../components/settings/atoms.jsx";

// relativeTime renders an ISO timestamp as a short "8s / 4m / 2h / 3d ago".
function relativeTime(iso) {
  if (!iso) return "syncing";
  const secs = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000));
  if (secs < 5) return "just now";
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function AddRepoBar({ onAdded }) {
  const [value, setValue] = useState("");
  const [state, setState] = useState("idle"); // idle | checking | success | error
  const [message, setMessage] = useState("");

  async function submit() {
    const repo = value.trim();
    if (!repo || state === "checking") return;
    setState("checking");
    setMessage("Verifying access on the server. This usually takes a second.");
    try {
      await addRepo(repo);
      setState("success");
      setMessage("Access confirmed. Polling started. PRs appear within a few seconds.");
      setValue("");
      onAdded();
    } catch (err) {
      setState("error");
      setMessage(err.message || "Could not add repository.");
    }
  }

  return (
    <div className="addrepo">
      <div className={"addbar " + state}>
        <span className="ab-prefix mono"><Icon name="github" size={13} /></span>
        <input
          className="ab-input mono"
          value={value}
          onChange={(e) => { setValue(e.target.value); if (state !== "idle") setState("idle"); }}
          onKeyDown={(e) => { if (e.key === "Enter") submit(); }}
          placeholder="owner/repo"
        />
        <div className="ab-status">
          {state === "checking" && <span className="ab-spin"><Icon name="loader" size={14} /></span>}
          {state === "success" && <span className="ab-ok"><Icon name="check" size={14} /></span>}
          {state === "error" && <span className="ab-err"><Icon name="alert" size={14} /></span>}
        </div>
        <button
          type="button"
          className={"btn" + (state === "idle" || state === "error" ? " primary" : "")}
          disabled={state === "checking"}
          onClick={submit}
        >
          {state === "checking" ? "Checking" : "Add repo"}
        </button>
      </div>
      {message ? (
        <div className={"ab-msg " + state}>
          {state === "success" && <Icon name="check" size={11} />}
          {state === "error" && <Icon name="alert" size={11} />}
          <span>{message}</span>
        </div>
      ) : null}
    </div>
  );
}

function RepoRow({ r, onChanged }) {
  const [busy, setBusy] = useState(false);
  const isError = r.health === "error";

  async function recheck() {
    setBusy(true);
    try { await recheckRepo(r.repo); onChanged(); } finally { setBusy(false); }
  }
  async function remove() {
    setBusy(true);
    try { await removeRepo(r.repo); onChanged(); } finally { setBusy(false); }
  }

  return (
    <div className={"repo-row" + (isError ? " is-error" : "")}>
      <div className="rr-name">
        <Icon name="github" size={13} />
        <span className="mono">{r.repo}</span>
        {r.profile ? <span className="rr-badge mono" title={`Review rules from the "${r.profile}" profile`}>{r.profile}</span> : null}
      </div>
      <div className="rr-sync mono">
        {isError ? (
          <span className="rr-sync-err" title={r.error || "server lost access"}>
            <Icon name="alert" size={11} /><span>{r.error || "server lost access"}</span>
          </span>
        ) : (
          relativeTime(r.synced_at)
        )}
      </div>
      <div className="rr-actions">
        {isError ? (
          <button type="button" className="btn" onClick={recheck} disabled={busy}>
            <Icon name="rerun" size={11} />Re-check
          </button>
        ) : null}
        <button
          type="button"
          className="btn ghost rr-remove"
          title="Remove from your dashboard"
          onClick={remove}
          disabled={busy}
          aria-label={"remove " + r.repo}
        >
          <Icon name="x" size={12} />
        </button>
      </div>
    </div>
  );
}

export default function SettingsRepos() {
  const { data: reposData, refetch: refetchRepos } = useApi(getRepos);
  const { data: suggData, refetch: refetchSuggestions } = useApi(getRepoSuggestions);
  const [adding, setAdding] = useState(null);

  const repos = reposData ?? [];
  const suggestions = suggData ?? [];

  function refreshAll() {
    refetchRepos();
    refetchSuggestions();
  }

  async function addSuggested(repo) {
    setAdding(repo);
    try { await addRepo(repo); refreshAll(); } catch { /* surfaced by the add bar normally; ignore here */ } finally { setAdding(null); }
  }

  const errorCount = repos.filter((r) => r.health === "error").length;

  return (
    <SettingsShell tab="repos" ready={reposData !== undefined}>
      <div className="set-section">
        <AddRepoBar onAdded={refreshAll} />
      </div>

      <div className="set-section">
        <div className="set-sec-head">
          <h3>Observing</h3>
          <span className="count mono">{repos.length} {repos.length === 1 ? "repository" : "repositories"}</span>
        </div>

        {repos.length === 0 ? (
          <div className="set-empty mono">
            <span>You don&apos;t observe any repositories yet.</span>
            <span>Add one above to start seeing its pull requests.</span>
          </div>
        ) : (
          <div className="repo-list">
            <div className="repo-list-head mono">
              <div>repository</div><div>last sync</div><div />
            </div>
            {repos.map((r) => <RepoRow key={r.repo} r={r} onChanged={refreshAll} />)}
          </div>
        )}

        {errorCount > 0 && (
          <div className="ai-block repos-note">
            {errorCount === 1 ? "A repository" : `${errorCount} repositories`} stopped responding. The server can no longer reach
            {errorCount === 1 ? " it" : " them"}, so their PRs are hidden from your groups. Re-check to retry, or remove.
          </div>
        )}
      </div>

      {suggestions.length > 0 && (
        <div className="set-section">
          <div className="set-sec-head">
            <h3>Your team also observes</h3>
            <span className="count mono">{suggestions.length} {suggestions.length === 1 ? "suggestion" : "suggestions"}</span>
          </div>
          <div className="sugg-grid two">
            {suggestions.map((s) => (
              <RepoSuggestion key={s.repo} repo={s.repo} observers={s.observers} onAdd={addSuggested} adding={adding === s.repo} />
            ))}
          </div>
        </div>
      )}
    </SettingsShell>
  );
}
