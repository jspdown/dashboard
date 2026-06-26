export default function Icon({ name, size = 14, style, stroke = 1.75, className }) {
  const props = {
    width: size, height: size, viewBox: "0 0 24 24",
    fill: "none", stroke: "currentColor",
    strokeWidth: stroke, strokeLinecap: "round", strokeLinejoin: "round",
    style, className, "aria-hidden": "true",
  };
  switch (name) {
    case "github":
      return <svg {...props} fill="currentColor" stroke="none"><path d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.55v-1.94c-3.2.69-3.87-1.54-3.87-1.54-.52-1.32-1.27-1.67-1.27-1.67-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.02 1.75 2.68 1.24 3.34.95.1-.74.4-1.24.73-1.53-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.29 1.18-3.09-.12-.29-.51-1.46.11-3.04 0 0 .97-.31 3.18 1.18a11.05 11.05 0 0 1 5.78 0c2.21-1.49 3.18-1.18 3.18-1.18.62 1.58.23 2.75.11 3.04.74.8 1.18 1.83 1.18 3.09 0 4.42-2.69 5.4-5.25 5.68.41.36.78 1.06.78 2.13v3.16c0 .31.21.66.79.55C20.21 21.39 23.5 17.08 23.5 12 23.5 5.65 18.35.5 12 .5z"/></svg>;
    case "slack":
      return <svg {...props} fill="currentColor" stroke="none"><path d="M5 14.5A1.5 1.5 0 1 1 3.5 13H5v1.5zm.75 0a1.5 1.5 0 0 1 3 0v3.75a1.5 1.5 0 0 1-3 0V14.5zM7.25 5A1.5 1.5 0 1 1 8.75 3.5V5H7.25zm0 .75a1.5 1.5 0 0 1 0 3H3.5a1.5 1.5 0 1 1 0-3h3.75zM18 7.25A1.5 1.5 0 1 1 19.5 8.75H18V7.25zm-.75 0a1.5 1.5 0 0 1-3 0V3.5a1.5 1.5 0 0 1 3 0v3.75zM15.75 18a1.5 1.5 0 1 1-1.5 1.5V18h1.5zm0-.75a1.5 1.5 0 0 1 0-3h3.75a1.5 1.5 0 1 1 0 3h-3.75zM10.75 12a1.5 1.5 0 1 1 1.5-1.5h1.5v1.5h-3z"/></svg>;
    case "cal":
      return <svg {...props}><rect x="3" y="4.5" width="18" height="17" rx="2"/><path d="M3 9.5h18M8 2.5v4M16 2.5v4"/></svg>;
    case "search":
      return <svg {...props}><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg>;
    case "filter":
      return <svg {...props}><path d="M4 5h16M7 12h10M10 19h4"/></svg>;
    case "sort":
      return <svg {...props}><path d="M7 3v18M4 7l3-3 3 3M17 21V3M14 17l3 3 3-3"/></svg>;
    case "flame":
      return <svg {...props} fill="currentColor" stroke="none" fillOpacity="0.92"><path d="M13.5 1.5C13.9 5.4 16.6 6.7 17.6 10c.8 2.6-.5 6-3.4 7.5 1-1.5 1-3 .1-4.6-.7 2-2.5 2.6-3.6 4.6-.9 1.7-.4 3.3.7 4-3.6-.5-6.4-3.4-6.4-7 0-3 2.1-4.7 3-6.7.5 1 1.1 1.7 2.1 2.2C9.6 7.6 11.5 5.4 13.5 1.5z"/></svg>;
    case "check":
      return <svg {...props}><path d="m20 6-11 11-5-5"/></svg>;
    case "x":
      return <svg {...props}><path d="M18 6 6 18M6 6l12 12"/></svg>;
    case "clock":
      return <svg {...props}><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>;
    case "chevron-down":
      return <svg {...props}><path d="m6 9 6 6 6-6"/></svg>;
    case "chevron-right":
      return <svg {...props}><path d="m9 6 6 6-6 6"/></svg>;
    case "circle":
      return <svg {...props}><circle cx="12" cy="12" r="9"/></svg>;
    case "circle-dot":
      return <svg {...props}><circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="3" fill="currentColor"/></svg>;
    case "merge":
      return <svg {...props}><circle cx="6" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="6" r="3"/><path d="M6 9v6"/><path d="M9 6c0 7 9 5 9 9"/></svg>;
    case "pr":
      return <svg {...props}><circle cx="6" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="18" r="3"/><path d="M6 9v6M18 9v6M9 6h3a3 3 0 0 1 3 3v6"/></svg>;
    case "comment":
      return <svg {...props}><path d="M21 12a8 8 0 0 1-12 7l-5 1 1-5a8 8 0 1 1 16-3z"/></svg>;
    case "zap":
      return <svg {...props}><path d="m13 2-9 12h7l-1 8 9-12h-7z"/></svg>;
    case "alert":
      return <svg {...props}><path d="M12 9v4M12 17h.01"/><path d="M10.3 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.7 3.86a2 2 0 0 0-3.4 0z"/></svg>;
    case "play":
      return <svg {...props}><path d="m6 4 14 8-14 8V4z" fill="currentColor"/></svg>;
    case "split":
      return <svg {...props}><path d="M16 3h5v5M21 3l-7 7M8 21H3v-5M3 21l7-7M21 16v5h-5M21 21l-7-7"/></svg>;
    case "trash":
      return <svg {...props}><path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6M10 11v6M14 11v6"/></svg>;
    case "bell":
      return <svg {...props}><path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9zM10.3 21a1.94 1.94 0 0 0 3.4 0"/></svg>;
    case "settings":
      return <svg {...props}><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>;
    case "rerun":
      return <svg {...props}><path d="M3 12a9 9 0 1 0 3-6.7L3 8M3 3v5h5"/></svg>;
    case "doc":
      return <svg {...props}><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M9 13h6M9 17h6M9 9h2"/></svg>;
    case "sliders":
      return <svg {...props}><line x1="21" x2="14" y1="4" y2="4"/><line x1="10" x2="3" y1="4" y2="4"/><line x1="21" x2="12" y1="12" y2="12"/><line x1="8" x2="3" y1="12" y2="12"/><line x1="21" x2="16" y1="20" y2="20"/><line x1="12" x2="3" y1="20" y2="20"/><line x1="14" x2="14" y1="2" y2="6"/><line x1="8" x2="8" y1="10" y2="14"/><line x1="16" x2="16" y1="18" y2="22"/></svg>;
    case "plus":
      return <svg {...props}><path d="M12 5v14M5 12h14"/></svg>;
    case "minus":
      return <svg {...props}><path d="M5 12h14"/></svg>;
    case "tag":
      return <svg {...props}><path d="M12.586 2.586A2 2 0 0 0 11.172 2H4a2 2 0 0 0-2 2v7.172a2 2 0 0 0 .586 1.414l8.704 8.704a2.426 2.426 0 0 0 3.42 0l6.58-6.58a2.426 2.426 0 0 0 0-3.42z"/><circle cx="7.5" cy="7.5" r=".5" fill="currentColor"/></svg>;
    case "users":
      return <svg {...props}><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>;
    case "git-branch":
      return <svg {...props}><line x1="6" x2="6" y1="3" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/></svg>;
    case "loader":
      return <svg {...props}><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>;
    case "folder":
      return <svg {...props}><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>;
    default:
      return <svg {...props}><circle cx="12" cy="12" r="3"/></svg>;
  }
}
