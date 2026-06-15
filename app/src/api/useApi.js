import { useState, useEffect, useCallback } from "react";

import { UnauthorizedError } from "./index.js";

/**
 * Generic hook for calling an async API function.
 *
 * A 401 (UnauthorizedError) means the proxy session expired; we force a full
 * reload so the proxy can re-auth, resetting React state along with it.
 *
 * Usage:
 *   const { data, loading, error, refetch } = useApi(getBriefing);
 *   const { data } = useApi(() => getPRs({ filter, sort }), [filter, sort]);
 */
export function useApi(fn, deps = []) {
  const [data, setData] = useState(undefined);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const run = useCallback(() => {
    setLoading(true);
    setError(null);
    return fn()
      .then((result) => {
        setData(result);
        setLoading(false);
        return result;
      })
      .catch((err) => {
        if (err instanceof UnauthorizedError) {
          window.location.reload();
          return;
        }
        setError(err);
        setLoading(false);
      });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  useEffect(() => { run(); }, [run]);

  return { data, loading, error, refetch: run };
}
