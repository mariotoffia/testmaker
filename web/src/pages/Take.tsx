import { useEffect, useRef, useState } from "react";
import { useTakeSession } from "../player/useTakeSession";
import { useCountdown } from "../player/useCountdown";
import { Countdown } from "../components/Countdown";
import { PlayerItem } from "../components/PlayerItem";
import { ScoreView } from "../player/ScoreView";
import { emptyAnswer, type Answer } from "../player/answer";
import type { ItemSnapshot } from "../api/types";

type Session = ReturnType<typeof useTakeSession>;

// InTestView owns the per-item hooks (working answer, countdowns). It is a
// component — not an inline branch of Take — so its hooks run unconditionally
// (rules of hooks) and reset cleanly when the presented item changes.
function InTestView({ s, item }: { s: Session; item: ItemSnapshot }) {
  const [answer, setAnswer] = useState<Answer>(() => emptyAnswer(item.ID, item.AnswerFormat));

  // Reset the working answer whenever the presented item changes.
  useEffect(() => { setAnswer(emptyAnswer(item.ID, item.AnswerFormat)); }, [item.ID, item.AnswerFormat]);

  // useCountdown captures onExpire once per deadline, but the selection changes
  // after that — read the latest answer through a ref so auto-submit records the
  // taker's actual choice (empty ⇒ wrong, the speeded convention, C10).
  const answerRef = useRef(answer);
  answerRef.current = answer;

  const perItem = useCountdown(s.deadline, () => s.submit(answerRef.current));
  const global = useCountdown(s.globalDeadline);

  return (
    <div className="mx-auto max-w-2xl p-6">
      <header className="mb-4 flex justify-between">
        <Countdown ms={global} label="Total" />
        <Countdown ms={perItem} label="This item" />
      </header>
      {s.error && <p className="mb-3 rounded bg-amber-50 p-2 text-sm text-amber-800">{s.error}</p>}
      <PlayerItem item={item} value={answer} onChange={setAnswer} onSubmit={() => s.submit(answer)} busy={s.busy} />
    </div>
  );
}

// Take is the public player route (/take). The taker's authority is the
// invite/session capability token in the URL fragment (#…), never the operator
// auth context (ADR-0005). It renders one view per session phase.
export default function Take() {
  const invite = window.location.hash.slice(1);
  const s = useTakeSession(invite);

  if (!invite) return <p className="p-8">This link is missing its invite token.</p>;

  if (s.phase === "in-test" && s.delivery?.Item) return <InTestView s={s} item={s.delivery.Item} />;
  if (s.phase === "complete") {
    return (
      <div className="mx-auto max-w-2xl p-6">
        <ScoreView sid={s.sid} token={s.token} />
      </div>
    );
  }

  // preview
  const p = s.preview;
  if (!p) return <p className="p-8">Loading…</p>;
  return (
    <div className="mx-auto max-w-2xl p-6">
      <h1 className="text-2xl font-semibold">{p.title}</h1>
      <p className="mt-2 text-slate-600">
        {p.itemCount} items{p.totalSeconds ? ` · ${Math.round(p.totalSeconds / 60)} min` : ""}
      </p>
      <button onClick={() => s.start()} className="mt-6 rounded bg-slate-800 px-5 py-2 text-white">
        Start
      </button>
    </div>
  );
}
