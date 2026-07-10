import { useCallback, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ApiError, api } from "../api/client";
import type { Answer } from "./answer";
import type { Delivery, InvitePreview, StartResponse } from "../api/types";

type Phase = "preview" | "in-test" | "complete";

// parseTime turns an RFC3339 stamp into a Date, treating Go's zero time
// (0001-01-01…) as "no deadline" (null).
function parseTime(s: string | undefined): Date | null {
  if (!s || s.startsWith("0001-01-01")) return null;
  const t = Date.parse(s);
  return Number.isNaN(t) ? null : new Date(t);
}

// useTakeSession is the player state machine: preview an invite, start a
// session (capturing the session token), then advance item-by-item until the
// plan is exhausted and the attempt completes. Time is never trusted from the
// client alone — deadlines come from the server's Delivery and are rendered
// against server-skew-corrected local time (Task 31).
export function useTakeSession(invite: string) {
  const [phase, setPhase] = useState<Phase>("preview");
  const [delivery, setDelivery] = useState<Delivery | null>(null);
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>("");

  const previewQ = useQuery({
    queryKey: ["invitePreview", invite],
    queryFn: () => api.previewInvite(invite),
    enabled: !!invite,
    retry: false,
  });

  const start = useCallback(async () => {
    const started: StartResponse = await api.startInvite(invite);
    setToken(started.SessionToken);
    setDelivery({ Session: started.Session, Item: started.Item, Deadline: started.Deadline });
    setPhase("in-test");
    // ponytail: we persist the session token/id so a future reload could resume,
    // but a true refresh-resume needs a server-side "re-present current item"
    // verb (client-supplied If-Match) — deferred to ROADMAP §6. Until then a
    // reload restarts at preview; we do not ship a dead "resume" button.
    sessionStorage.setItem("tm.session", JSON.stringify({ token: started.SessionToken, sid: started.Session.ID }));
  }, [invite]);

  const sid = delivery?.Session.ID ?? "";

  const submit = useCallback(
    async (answer: Answer) => {
      if (busy || !sid) return;
      setBusy(true);
      setError("");
      try {
        const next = await api.answer(token, sid, answer);
        if (next.Session.Presented.ItemID === "") {
          await api.complete(token, sid);
          setDelivery(next);
          setPhase("complete");
        } else {
          setDelivery(next);
        }
      } catch (e) {
        // 409 = a concurrent writer (another tab) already advanced this session:
        // keep the current item and tell the taker their other tab owns it (C3).
        if (e instanceof ApiError && e.status === 409) {
          setError("This attempt is being continued in another tab or window.");
        } else {
          setError(e instanceof Error ? e.message : "answer failed");
        }
      } finally {
        setBusy(false);
      }
    },
    [busy, sid, token],
  );

  const deadline = useMemo(() => parseTime(delivery?.Deadline), [delivery?.Deadline]);
  const globalDeadline = useMemo(() => {
    const s = delivery?.Session;
    if (!s) return null;
    const started = parseTime(s.StartedAt);
    if (!started || !s.Timing.Total) return null; // Total 0 = untimed
    return new Date(started.getTime() + s.Timing.Total / 1_000_000); // ns → ms
  }, [delivery?.Session]);

  return {
    phase, preview: previewQ.data as InvitePreview | undefined, previewError: previewQ.error as ApiError | null,
    delivery, deadline, globalDeadline, sid, token, busy, error, start, submit,
  };
}
