import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Score } from "../api/types";

function ms(ns: number) { return Math.round(ns / 1_000_000); }

// ScoreView fetches and renders the completed attempt's score: raw/max, speed,
// the normed band/IQ/percentile (or a raw-only note when the test carries no
// norms), and the per-item feedback (given vs correct + explanation).
export function ScoreView({ sid, token }: { sid: string; token: string }) {
  const q = useQuery({ queryKey: ["score", sid], queryFn: () => api.score(token, sid), retry: false });
  if (q.isLoading) return <p>Scoring…</p>;
  if (q.error || !q.data) return <p className="text-red-600">Could not load your score.</p>;
  const s: Score = q.data;
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Your result</h1>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
        <div className="rounded border p-4"><div className="text-3xl font-semibold">{s.Raw}/{s.Max}</div><div className="text-sm text-slate-500">Correct</div></div>
        {s.Normed ? (
          <>
            <div className="rounded border p-4"><div className="text-3xl font-semibold">{s.ScaledIQ}</div><div className="text-sm text-slate-500">Scaled IQ</div></div>
            <div className="rounded border p-4"><div className="text-3xl font-semibold">{s.Percentile.toFixed(1)}</div><div className="text-sm text-slate-500">Percentile · {s.Band}</div></div>
          </>
        ) : (
          <div className="col-span-2 rounded border p-4 text-sm text-slate-500">Raw score only — this test carries no norms.</div>
        )}
      </div>
      <p className="text-sm text-slate-600">
        Speed: {ms(s.Speed.Total)} ms total, {ms(s.Speed.Mean)} ms/item, {s.Speed.CorrectPerMinute.toFixed(1)} correct/min.
      </p>
      <div className="space-y-2">
        <h2 className="font-semibold">Review</h2>
        {(s.Items ?? []).map((f) => (
          <div key={f.ItemID} className={`rounded border p-3 ${f.Correct ? "border-green-300" : "border-red-300"}`}>
            <div className="text-sm">{f.Correct ? "✓ Correct" : `✗ You: ${f.Given || "—"} · Correct: ${f.CorrectAnswer}`}</div>
            {f.Explanation && <div className="mt-1 text-sm text-slate-600">{f.Explanation}</div>}
          </div>
        ))}
      </div>
    </div>
  );
}
