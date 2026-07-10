import { Link, useParams } from "react-router-dom";
import { useSource } from "../api/hooks";
import { Async } from "../components/Async";

// SourceDetail shows one source's provenance and license, and hosts the ingest
// actions (Task 25 adds the ingest / ingest-llm buttons).
export default function SourceDetail() {
  const { id = "" } = useParams();
  const source = useSource(id);
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
          </div>
        )}
      </Async>
    </div>
  );
}
