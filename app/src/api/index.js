// API layer: fetches from the Go HTTP API.
// In dev, /api/* is proxied to http://localhost:8080 by Vite.

// UnauthorizedError surfaces a 401 from the API. With oauth2-proxy in front
// this only happens if the upstream session expired between page load and the
// fetch. useApi catches it and forces a reload so the proxy can re-auth.
export class UnauthorizedError extends Error {
  constructor(path) {
    super(`unauthorized: ${path}`);
    this.name = "UnauthorizedError";
    this.path = path;
  }
}

async function apiFetch(path, opts) {
  const res = await fetch(path, opts);
  if (res.status === 401) {
    throw new UnauthorizedError(path);
  }
  if (!res.ok) {
    throw new Error(`${path}: ${res.status}`);
  }
  return res;
}

/**
 * Returns all open/recent PRs.
 * @param {{ filter?: string, sort?: string }} opts
 */
export async function getPRs({ filter = "all", sort = "priority" } = {}) {
  const qs = new URLSearchParams({ filter, sort });
  const res = await apiFetch(`/api/prs?${qs}`);
  return res.json();
}

/**
 * Records that the viewer has opened (or explicitly dismissed) a PR.
 * Server captures the current comments_count and head_sha as the
 * baseline for future "what's new" diffs.
 * @param {number} id PR github_id, taken from the `id` field of getPRs.
 */
export async function markPRViewed(id) {
  await apiFetch(`/api/prs/${id}/viewed`, { method: "POST" });
}

/** Returns the VCS info embedded in the running API binary. */
export async function getBuildInfo() {
  const res = await apiFetch("/api/build");
  return res.json();
}

/**
 * Returns the runtime UI configuration: stale/merged windows and the
 * review-policy labels used to render group descriptions. The viewer
 * identity is delivered separately through /api/me.
 */
export async function getConfig() {
  const res = await apiFetch("/api/config");
  return res.json();
}

/**
 * Returns the signed-in user: { login, avatar_url }. Populated by the
 * dashboard's TrustedHeader middleware from oauth2-proxy's headers.
 */
export async function getMe() {
  const res = await apiFetch("/api/me");
  return res.json();
}

// ---- Per-user settings: repositories & review rules ----

// ApiError carries the HTTP status and the server's message so the settings
// screens can show "not found / no access" inline rather than a generic failure.
export class ApiError extends Error {
  constructor(status, message) {
    super(message || `request failed: ${status}`);
    this.name = "ApiError";
    this.status = status;
  }
}

// mutate is a fetch wrapper for write endpoints: it preserves UnauthorizedError
// (so useApi can re-auth) and surfaces the server's message via ApiError.
async function mutate(path, opts) {
  const res = await fetch(path, opts);
  if (res.status === 401) throw new UnauthorizedError(path);
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new ApiError(res.status, body.trim());
  }
  return res;
}

/** Returns the repos the viewer observes, each with health and PR counts. */
export async function getRepos() {
  const res = await apiFetch("/api/settings/repos");
  return res.json();
}

/** Returns repos the viewer's teammates observe but the viewer hasn't added. */
export async function getRepoSuggestions() {
  const res = await apiFetch("/api/settings/repos/suggestions");
  return res.json();
}

/**
 * Adds a repo to the viewer's dashboard after the server verifies access.
 * Throws ApiError on a bad slug (400), duplicate (409), or no access (422).
 * @param {string} repo owner/name slug
 */
export async function addRepo(repo) {
  const res = await mutate("/api/settings/repos", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ repo }),
  });
  return res.json();
}

/** Removes a repo from the viewer's dashboard. @param {string} repo owner/name */
export async function removeRepo(repo) {
  await mutate(`/api/settings/repos/${repo}`, { method: "DELETE" });
}

/** Re-verifies access to a repo that lost it, restarting polling on success. */
export async function recheckRepo(repo) {
  await mutate(`/api/settings/repos/${repo}/recheck`, { method: "POST" });
}

/** Returns the viewer's review rules (the editable Review rules screen shape). */
export async function getRules() {
  const res = await apiFetch("/api/settings/rules");
  return res.json();
}

/** Replaces the viewer's review rules and returns the saved (normalized) shape. */
export async function saveRules(rules) {
  const res = await mutate("/api/settings/rules", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(rules),
  });
  return res.json();
}
