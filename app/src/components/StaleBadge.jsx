export default function StaleBadge({ age, threshold }) {
  if (age < threshold) return null;
  const title =
    `Open for ${age} day${age === 1 ? "" : "s"} without being merged. ` +
    `PRs older than ${threshold} days tend to accumulate merge conflicts, ` +
    `lose reviewer context, and delay shipping. Worth nudging or closing.`;
  return (
    <span className="badge danger" title={title}>
      <span className="dot danger" />stale {age}d
    </span>
  );
}
