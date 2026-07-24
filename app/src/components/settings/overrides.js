// Reviewer-override rows need stable React keys while their labels are edited
// (and may be blank or duplicated mid-edit), so each carries a client-only _id,
// stripped before saving. The counter is shared so ids stay unique across the
// initial load and rows added later.
let seq = 0;
export const nextOverrideId = () => ++seq;

/** withOverrideIds tags reviewer-override rows with stable client-only keys. */
export function withOverrideIds(overrides = []) {
  return overrides.map((o) => ({ ...o, _id: nextOverrideId() }));
}

/** stripOverrideIds drops the client-only _id and half-typed (blank-label) rows
 * before saving, so they don't trip the server's validation. */
export function stripOverrideIds(overrides = []) {
  return overrides.filter((o) => o.label.trim() !== "").map((o) => ({ label: o.label, reviewers: o.reviewers }));
}
