export default function StaleBadge({ stale, age }) {
  if (!stale) return null;
  const title =
    `Open for ${age} day${age === 1 ? "" : "s"} without being merged. ` +
    `Past its profile's stale window, PRs accumulate merge conflicts, ` +
    `lose reviewer context, and delay shipping. Worth nudging or closing.`;
  return (
    <span className="badge danger" title={title}>
      <span className="dot danger" />stale {age}d
    </span>
  );
}
