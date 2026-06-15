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
