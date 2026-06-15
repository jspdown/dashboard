import { createContext, useContext } from "react";

// Context populated by <ConfigProvider>. Split out from the provider file so
// the JSX file only exports components (react-refresh "components-only" rule).
export const ConfigContext = createContext(null);

export function useConfig() {
  const cfg = useContext(ConfigContext);
  if (!cfg) {
    throw new Error("useConfig() called outside <ConfigProvider> — wrap the tree in main.jsx");
  }
  return cfg;
}
