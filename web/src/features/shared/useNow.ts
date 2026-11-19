// A clock for a countdown or a wait on screen: performance.now(), read again every period while mounted.
import { useEffect, useState } from 'react';

export function useNow(periodMs: number): number {
  const [now, setNow] = useState(() => performance.now());
  useEffect(() => {
    const id = setInterval(() => setNow(performance.now()), periodMs);
    return () => clearInterval(id);
  }, [periodMs]);
  return now;
}
