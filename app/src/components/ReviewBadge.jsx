// ReviewBadge shows approvals as n/m, with color signalling merge-readiness.
// state comes from the backend's merge_state (see MergeReadiness in rules.go),
// so policy lives in one place and we just render it.
export default function ReviewBadge({ approvals, required, state }) {
  if (!state) return null; // merged or otherwise not applicable

  const frac = `${approvals}/${required}`;
  switch (state) {
    case "ready":
      return (
        <span className="badge success" title={`Ready to merge: ${frac} approvals, CI green`}>
          <span className="dot success" />{frac}
        </span>
      );
    case "needs_approval": {
      const left = Math.max(required - approvals, 0);
      const title = `Needs ${left} more approval${left === 1 ? "" : "s"} (${frac})`;
      return (
        <span className="badge warn" title={title}>
          <span className="dot warn" />{frac}
        </span>
      );
    }
    case "ci_pending":
      return (
        <span className="badge warn" title={`Approved ${frac}, waiting on CI`}>
          <span className="dot warn" />{frac}
        </span>
      );
    case "blocked":
      return (
        <span className="badge danger" title={`Blocked: changes requested or CI failing (${frac} approvals)`}>
          <span className="dot danger" />{frac}
        </span>
      );
    case "draft":
      return (
        <span className="badge" title="Draft, not ready for review">
          <span className="dot" />draft
        </span>
      );
    default:
      return null;
  }
}
