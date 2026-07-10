import { Link, useParams } from "react-router-dom";
import { useTest } from "../api/hooks";
import { Async } from "../components/Async";
import { InviteButton } from "../components/InviteButton";
import { NS_PER_SEC, type Ns } from "../api/types";

// secs renders a wire duration (nanoseconds; 0 = untimed) as a human string.
function secs(ns: Ns): string {
  return ns > 0 ? `${Math.round(ns / NS_PER_SEC)}s` : "untimed";
}

export default function TestDetail() {
  const { id = "" } = useParams();
  const test = useTest(id);
  return (
    <div className="max-w-2xl">
      <Link to="/tests" className="text-sm text-blue-700 hover:underline">← Tests</Link>
      <Async query={test}>
        {(t) => (
          <div className="mt-3 space-y-5">
            <div>
              <h1 className="text-xl font-semibold">{t.Title}</h1>
              <p className="text-sm text-slate-500">
                <span className="font-mono">{t.ID}</span> · {t.Policy} · total {secs(t.Timing.Total)} · per-item {secs(t.Timing.PerItem)}
              </p>
            </div>

            <div>
              <h2 className="mb-2 text-sm font-medium">Sections</h2>
              <table className="w-full text-sm">
                <thead className="text-left text-slate-500">
                  <tr><th className="py-1">Title</th><th>Family</th><th>Items</th><th>Total</th><th>Per-item</th></tr>
                </thead>
                <tbody>
                  {(t.Sections ?? []).map((s, i) => (
                    <tr key={i} className="border-t">
                      <td className="py-1">{s.Title || "—"}</td>
                      <td>{s.Family}</td>
                      <td>{(s.Items ?? []).length}</td>
                      <td>{secs(s.Timing.Total)}</td>
                      <td>{secs(s.Timing.PerItem)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div className="border-t pt-4">
              <h2 className="mb-2 text-sm font-medium">Invite a taker</h2>
              <InviteButton testId={t.ID} />
              {/* ponytail: operator direct-start (POST /api/tests/:id/sessions) is deferred to
                  Phase 8 — the player is invite-driven and has no landing for a pre-started
                  session yet. Add the button when Take.tsx can receive one. */}
            </div>
          </div>
        )}
      </Async>
    </div>
  );
}
