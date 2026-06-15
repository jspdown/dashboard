const COLORS = [
  ["#5b6cff", "#fff"], ["#e879f9", "#fff"], ["#22d3ee", "#0a0a0a"],
  ["#f59e0b", "#0a0a0a"], ["#34d399", "#0a0a0a"], ["#f472b6", "#fff"],
  ["#a78bfa", "#fff"], ["#fb923c", "#0a0a0a"], ["#60a5fa", "#0a0a0a"],
];

export default function Avatar({ name, size = 22 }) {
  const initials = name.split(/[\s_-]+/).filter(Boolean).slice(0, 2).map(s => s[0]).join("").toUpperCase();
  let hash = 0;
  for (let i = 0; i < name.length; i++) hash = (hash * 31 + name.charCodeAt(i)) & 0xffffffff;
  const [bg, fg] = COLORS[Math.abs(hash) % COLORS.length];
  return (
    <span
      className="avatar"
      title={name}
      style={{
        width: size, height: size,
        background: bg, color: fg,
        fontSize: Math.round(size * 0.42),
        display: "inline-grid", placeItems: "center",
        borderRadius: "50%",
        fontFamily: "var(--font-mono)",
        border: "1px solid var(--border-1)",
        flexShrink: 0,
      }}
    >
      {initials}
    </span>
  );
}
