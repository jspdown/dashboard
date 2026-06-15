export default function CIBadge({ status }) {
  if (status === "passing") return <span className="badge success" title="CI passing"><span className="dot success" />passing</span>;
  if (status === "failing") return <span className="badge danger" title="CI failing"><span className="dot danger" />failing</span>;
  if (status === "pending") return <span className="badge warn" title="CI running"><span className="dot warn" />pending</span>;
  if (status === "none")    return <span className="badge" title="No CI"><span className="dot" />—</span>;
  return null;
}
