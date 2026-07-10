import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import { useApiToken } from "../api/hooks";

export default function Generate() {
  const token = useApiToken();
  const qc = useQueryClient();
  const [form, setForm] = useState({ testType: "A2", difficulty: 2, count: 5, seed: 1 });
  const gen = useMutation({
    mutationFn: () => api.generate(token, form),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["items"] }),
  });
  return (
    <div className="max-w-md">
      <h1 className="mb-4 text-xl font-semibold">Generate figural items</h1>
      <form
        className="space-y-3"
        onSubmit={(e) => { e.preventDefault(); gen.mutate(); }}
      >
        <label className="block text-sm">Type
          <select className="mt-1 w-full rounded border px-2 py-1"
            value={form.testType} onChange={(e) => setForm({ ...form, testType: e.target.value })}>
            {["A1", "A2", "A3", "A4"].map((t) => <option key={t}>{t}</option>)}
          </select>
        </label>
        {(["difficulty", "count", "seed"] as const).map((k) => (
          <label key={k} className="block text-sm capitalize">{k}
            <input type="number" className="mt-1 w-full rounded border px-2 py-1"
              value={form[k]} onChange={(e) => {
                const n = Number(e.target.value);
                if (!Number.isNaN(n)) setForm({ ...form, [k]: n });
              }} />
          </label>
        ))}
        <button className="rounded bg-slate-800 px-4 py-2 text-white disabled:opacity-50" disabled={gen.isPending}>
          {gen.isPending ? "Generating…" : "Generate"}
        </button>
      </form>
      {gen.isSuccess && <p className="mt-3 text-sm text-green-700">Generated. The bank has been updated.</p>}
      {gen.isError && <p className="mt-3 text-sm text-red-600">Generation failed.</p>}
    </div>
  );
}
