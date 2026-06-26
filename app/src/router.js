import { useSyncExternalStore } from "react";

// A minimal hash router. The app is a single static bundle served behind a SPA
// fallback, so hash routes ("#/settings/repos") need no server config and never
// trigger a full reload. Enough for the handful of screens we have; reach for a
// real router only if routing grows.

function currentPath() {
  const hash = window.location.hash.replace(/^#/, "");
  return hash || "/";
}

function subscribe(callback) {
  window.addEventListener("hashchange", callback);
  return () => window.removeEventListener("hashchange", callback);
}

/** useRoute returns the current hash path, re-rendering on navigation. */
export function useRoute() {
  return useSyncExternalStore(subscribe, currentPath);
}

/** navigate sets the hash route. Prefer plain <a href="#/path"> where possible. */
export function navigate(path) {
  window.location.hash = path;
}
