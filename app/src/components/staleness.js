export const stalenessBg = (days) => {
  if (days < 1) return "var(--stale-0)";
  if (days < 3) return "var(--stale-1)";
  if (days < 5) return "var(--stale-2)";
  if (days < 8) return "var(--stale-3)";
  return "var(--stale-4)";
};

export const stalenessLabel = (days) => {
  if (days === 0) return "today";
  if (days === 1) return "1d";
  return `${days}d`;
};
