import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSource, useApiToken } from "../api/hooks";
import { Async } from "../components/Async";
import { api } from "../api/client";
import type { IngestReport, Job } from "../api/types";

// isJob distinguishes the async 202 envelope (a Job, has a state) from the sync
// 200 IngestReport — the endpoints share one client method (C6 / ADR-0007).
function isJob(r: Job | IngestReport): r is Job {
  return "state" in r;
}

// SourceDetail shows one source's provenance and license, and hosts the ingest
// actions. Sync ingest shows the counts inline; an async ingest 202s to a Job
// and navigates to the jobs board to watch it run.
export default function SourceDetail() {
  const { id = "" } = useParams();
  const token = useApiToken();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const source = useSource(id);
  const [asyncMode, setAsyncMode] = useState(false);
  const [report, setReport] = useState<IngestReport | null>(null);

  const onResult = (r: Job | IngestReport) => {
    // Ingest changed the bank: refresh this source's item count and the item /
    // source lists so the UI doesn't sit next to a stale "Bank items" figure.
    qc.invalidateQueries({ queryKey: ["source", id] });
    qc.invalidateQueries({ queryKey: ["items"] });
    qc.invalidateQueries({ queryKey: ["sources"] });
    if (isJob(r)) navigate("/jobs");
    else setReport(r);
  };
  const ingest = useMutation({
    mutationFn: () => api.ingest(token, id, { async: asyncMode }),
    onSuccess: onResult,
  });
  const ingestLLM = useMutation({
    mutationFn: () => api.ingestLLM(token, id, { async: asyncMode }),
    onSuccess: onResult,
  });
  const busy = ingest.isPending || ingestLLM.isPending;

  return (
    <div className="max-w-2xl">
      <Link to="/sources" className="text-sm text-blue-700 hover:underline">← Sources</Link>
      <Async query={source}>
        {(s) => (
          <div className="mt-3 space-y-4">
            <h1 className="text-xl font-semibold">{s.Name}</h1>
            <dl className="grid grid-cols-[9rem_1fr] gap-y-1 text-sm">
              <dt className="text-slate-500">Provider</dt>
              <dd>{s.Provider}</dd>
              <dt className="text-slate-500">License</dt>
              <dd>{s.License.Category} — redistributable: {s.License.Redistributable}</dd>
              <dt className="text-slate-500">Extraction</dt>
              <dd>{s.Extraction.Method || "—"}</dd>
              <dt className="text-slate-500">Test types</dt>
              <dd>{(s.TestTypes ?? []).join(", ") || "—"}</dd>
              <dt className="text-slate-500">Families</dt>
              <dd>{(s.Families ?? []).join(", ") || "—"}</dd>
              <dt className="text-slate-500">Bank items</dt>
              <dd>{s.ItemCount}</dd>
            </dl>

            <div className="space-y-3 border-t pt-4">
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={asyncMode} onChange={(e) => setAsyncMode(e.target.checked)} />
                Run as a background job
              </label>
              <div className="flex gap-2">
                <button
                  onClick={() => { setReport(null); ingest.mutate(); }}
                  disabled={busy}
                  className="rounded bg-slate-800 px-4 py-2 text-sm text-white disabled:opacity-50"
                >
                  {ingest.isPending ? "Ingesting…" : "Ingest"}
                </button>
                <button
                  onClick={() => { setReport(null); ingestLLM.mutate(); }}
                  disabled={busy}
                  className="rounded border px-4 py-2 text-sm disabled:opacity-50"
                >
                  {ingestLLM.isPending ? "Ingesting…" : "Ingest (LLM)"}
                </button>
              </div>
              {ingest.isError && <p className="text-sm text-red-600">Ingest failed.</p>}
              {ingestLLM.isError && (
                <p className="text-sm text-red-600">LLM ingest failed (the server may have no LLM backend configured).</p>
              )}
              {report && (
                <p className="text-sm text-green-700">
                  Saved {report.Saved} of {report.Normalized} normalized ({report.Skipped} skipped).
                </p>
              )}
            </div>
          </div>
        )}
      </Async>
    </div>
  );
}
