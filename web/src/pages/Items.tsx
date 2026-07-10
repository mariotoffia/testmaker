import { useState } from "react";
import { Link } from "react-router-dom";
import { useItems } from "../api/hooks";
import { Async } from "../components/Async";

const families = ["logical", "numerical", "verbal", "spatial", "speed"];

export default function Items() {
  const [family, setFamily] = useState("");
  const [testType, setTestType] = useState("");
  const [minDiff, setMinDiff] = useState("");
  const [maxDiff, setMaxDiff] = useState("");

  const params = new URLSearchParams();
  if (family) params.set("family", family);
  if (testType) params.set("testType", testType);
  if (minDiff) params.set("minDifficulty", minDiff);
  if (maxDiff) params.set("maxDifficulty", maxDiff);
  const q = params.toString() ? `?${params}` : "";
  const items = useItems(q);

  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold">Item bank</h1>
      <div className="mb-3 flex flex-wrap gap-2">
        <select value={family} onChange={(e) => setFamily(e.target.value)} className="rounded border px-2 py-1 text-sm" aria-label="filter by family">
          <option value="">all families</option>
          {families.map((f) => <option key={f} value={f}>{f}</option>)}
        </select>
        <input value={testType} onChange={(e) => setTestType(e.target.value)} placeholder="test type" className="w-28 rounded border px-2 py-1 text-sm" aria-label="filter by test type" />
        <input value={minDiff} onChange={(e) => setMinDiff(e.target.value)} type="number" placeholder="min diff" className="w-24 rounded border px-2 py-1 text-sm" aria-label="minimum difficulty" />
        <input value={maxDiff} onChange={(e) => setMaxDiff(e.target.value)} type="number" placeholder="max diff" className="w-24 rounded border px-2 py-1 text-sm" aria-label="maximum difficulty" />
      </div>
      <Async query={items}>
        {(page) =>
          page.items.length === 0 ? (
            <p className="text-slate-500">No items match. Generate figural items or ingest a source.</p>
          ) : (
            <table className="w-full text-sm">
              <thead className="text-left text-slate-500">
                <tr><th className="py-1">ID</th><th>Type</th><th>Family</th><th>Format</th><th>Difficulty</th></tr>
              </thead>
              <tbody>
                {page.items.map((it) => (
                  <tr key={it.ID} className="border-t">
                    <td className="py-1"><Link className="font-mono text-blue-700 hover:underline" to={`/items/${it.ID}`}>{it.ID}</Link></td>
                    <td>{it.TestType}</td>
                    <td>{it.Family}</td>
                    <td>{it.AnswerFormat}</td>
                    <td>{it.Difficulty.Band}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )
        }
      </Async>
    </div>
  );
}
