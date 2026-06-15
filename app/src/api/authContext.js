import { createContext, useContext } from "react";

// AuthContext carries the signed-in user's identity plus a refresh callback.
// AuthProvider populates it at boot and re-fetches on 401.
export const AuthContext = createContext(null);

export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) {
    throw new Error("useAuth() called outside <AuthProvider>");
  }
  return value;
}
