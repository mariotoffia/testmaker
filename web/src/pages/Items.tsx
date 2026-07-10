import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useItems, useApiToken } from "../api/hooks";
import { api, ApiError } from "../api/client";
import { Async } from "../components/Async";

const families = ["logical", "numerical", "verbal", "spatial", "speed"];

const errText = (e: unknown) => (e instanceof ApiError ? e.message : "unexpected error");

export default function Items() {
  const [family, setFamily] = useState("");
  const [testType, setTestType] = useState("");
  const [minDiff, setMinDiff] = useState("");
  const [maxDiff, setMaxDiff] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const token = useApiToken();
  const qc = useQueryClient();

  const params = new URLSearchParams();
  if (family) params.set("family", family);
  if (testType) params.set("testType", testType);
  if (minDiff) params.set("minDifficulty", minDiff);
  if (maxDiff) params.set("maxDifficulty", maxDiff);
  const q = params.toString() ? `?${params}` : "";
  const items = useItems(q);

  // Bulk delete fires one idempotent DELETE per selected id. onSettled always
  // refetches so the list reflects server truth even on a partial failure;
  // selection clears only when every delete succeeded.
  const del = useMutation({
    mutationFn: (ids: string[]) => Promise.all(ids.map((id) => api.deleteItem(token, id))),
    onSettled: () => qc.invalidateQueries({ queryKey: ["items"] }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["sources"] });
      setSelected(new Set());
    },
  });

  const setOne = (id: string, on: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });

  const onDelete = () => {
    const ids = [...selected];
    if (ids.length === 0) return;
    if (!window.confirm(`Delete ${ids.length} item${ids.length === 1 ? "" : "s"}? This cannot be undone.`)) return;
    del.mutate(ids);
  };

  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold">Item bank</h1>
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <select value={family} onChange={(e) => setFamily(e.target.value)} className="rounded border px-2 py-1 text-sm" aria-label="filter by family">
          <option value="">all families</option>
          {families.map((f) => <option key={f} value={f}>{f}</option>)}
        </select>
        <input value={testType} onChange={(e) => setTestType(e.target.value)} placeholder="test type" className="w-28 rounded border px-2 py-1 text-sm" aria-label="filter by test type" />
        <input value={minDiff} onChange={(e) => setMinDiff(e.target.value)} type="number" placeholder="min diff" className="w-24 rounded border px-2 py-1 text-sm" aria-label="minimum difficulty" />
        <input value={maxDiff} onChange={(e) => setMaxDiff(e.target.value)} type="number" placeholder="max diff" className="w-24 rounded border px-2 py-1 text-sm" aria-label="maximum difficulty" />
        <button
          onClick={onDelete}
          disabled={selected.size === 0 || del.isPending}
          className="ml-auto rounded bg-red-700 px-3 py-1 text-sm text-white disabled:opacity-40"
        >
          {del.isPending ? "Deleting…" : `Delete selected (${selected.size})`}
        </button>
      </div>
      {del.isError && <p className="mb-2 text-sm text-red-600">Delete failed: {errText(del.error)}.</p>}
      <Async query={items}>
        {(page) => {
          if (page.items.length === 0)
            return <p className="text-slate-500">No items match. Generate figural items or ingest a source.</p>;
          const allSelected = page.items.every((it) => selected.has(it.ID));
          const toggleAll = (on: boolean) =>
            setSelected((prev) => {
              const next = new Set(prev);
              for (const it of page.items) {
                if (on) next.add(it.ID);
                else next.delete(it.ID);
              }
              return next;
            });
          return (
            <table className="w-full text-sm">
              <thead className="text-left text-slate-500">
                <tr>
                  <th className="w-8 py-1">
                    <input type="checkbox" aria-label="select all" checked={allSelected} onChange={(e) => toggleAll(e.target.checked)} />
                  </th>
                  <th className="py-1">ID</th><th>Type</th><th>Family</th><th>Format</th><th>Difficulty</th>
                </tr>
              </thead>
              <tbody>
                {page.items.map((it) => (
                  <tr key={it.ID} className="border-t">
                    <td className="py-1">
                      <input type="checkbox" aria-label={`select ${it.ID}`} checked={selected.has(it.ID)} onChange={(e) => setOne(it.ID, e.target.checked)} />
                    </td>
                    <td className="py-1"><Link className="font-mono text-blue-700 hover:underline" to={`/items/${it.ID}`}>{it.ID}</Link></td>
                    <td>{it.TestType}</td>
                    <td>{it.Family}</td>
                    <td>{it.AnswerFormat}</td>
                    <td>{it.Difficulty.Band}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          );
        }}
      </Async>
    </div>
  );
}
