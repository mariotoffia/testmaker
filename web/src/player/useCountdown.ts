import { useEffect, useRef, useState } from "react";
import { serverSkewMs } from "../api/client";

// useCountdown returns milliseconds remaining until deadline, corrected for
// server clock skew, ticking ~4×/sec. It fires onExpire exactly once when it
// first hits zero. A null deadline (untimed section/test) returns null and does
// nothing. Skew (server − local) keeps a wrong local clock from mis-timing a
// speeded test (C10).
export function useCountdown(deadline: Date | null, onExpire?: () => void): number | null {
  const [remaining, setRemaining] = useState<number | null>(() =>
    deadline ? Math.max(0, deadline.getTime() - (Date.now() + serverSkewMs())) : null,
  );
  const fired = useRef(false);

  useEffect(() => {
    fired.current = false;
    if (!deadline) {
      setRemaining(null);
      return;
    }
    const tick = () => {
      const left = Math.max(0, deadline.getTime() - (Date.now() + serverSkewMs()));
      setRemaining(left);
      if (left === 0 && !fired.current) {
        fired.current = true;
        onExpire?.();
      }
    };
    tick();
    const h = setInterval(tick, 250);
    return () => clearInterval(h);
    // onExpire is intentionally excluded: it is captured per item; a new
    // deadline (new item) resets the effect and the fired guard.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deadline]);

  return remaining;
}
