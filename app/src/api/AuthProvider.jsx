import { useCallback, useEffect, useState } from "react";

import { AuthContext } from "./authContext.js";
import { UnauthorizedError, getMe } from "./index.js";

// signOutURL clears the upstream proxy session. oauth2-proxy serves this;
// in prod requests reach it through Traefik's forwardauth wiring.
const signOutURL = "/oauth2/sign_out?rd=%2F";

// AuthProvider fetches /api/me at boot and exposes the identity via useAuth().
// The proxy has already authenticated by the time the SPA loads, so this just
// propagates identity, it doesn't gate.
//
// A 401 on /api/me means the proxy session expired between page load and the
// fetch. We force a full reload; the proxy redirects to sign-in and drops the
// user back here once re-authenticated.
export function AuthProvider({ children }) {
  const [me, setMe] = useState(null);
  const [error, setError] = useState(null);

  const refresh = useCallback(async () => {
    try {
      const next = await getMe();
      setMe(next);
      return next;
    } catch (err) {
      if (err instanceof UnauthorizedError) {
        window.location.reload();
        return null;
      }
      setError(err);
      return null;
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  if (error) {
    return (
      <div style={{ padding: "1rem", color: "var(--state-danger)" }}>
        Failed to load auth: {String(error.message ?? error)}
      </div>
    );
  }
  if (!me) return null;

  const value = {
    login: me.login,
    avatarURL: me.avatar_url ?? null,
    signOut: () => { window.location.href = signOutURL; },
    refresh,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
