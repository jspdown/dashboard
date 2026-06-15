import { useEffect, useState } from "react";

import { ConfigContext } from "./configContext.js";
import { UnauthorizedError, getConfig } from "./index.js";

export function ConfigProvider({ children }) {
  const [config, setConfig] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    let cancelled = false;
    getConfig()
      .then((cfg) => {
        if (!cancelled) setConfig(cfg);
        return cfg;
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof UnauthorizedError) {
          window.location.reload();
          return;
        }
        setError(err);
      });
    return () => { cancelled = true; };
  }, []);

  if (error) {
    return (
      <div style={{ padding: "1rem", color: "var(--state-danger)" }}>
        Failed to load dashboard config: {String(error.message ?? error)}
      </div>
    );
  }
  if (!config) return null;

  return <ConfigContext.Provider value={config}>{children}</ConfigContext.Provider>;
}
