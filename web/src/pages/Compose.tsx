import { useState, type ChangeEvent } from "react";
import { Link } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";
import { api } from "../api/client";
import { useApiToken } from "../api/hooks";
import type { DeliveryPolicy } from "../api/types";

interface SectionForm {
  title: string;
  family: string;
  totalSeconds: number;
  perItemSeconds: number;
  minDifficulty: number;
  maxDifficulty: number;
}

const families = ["logical", "numerical", "verbal", "spatial", "speed"];
const emptySection = (): SectionForm => ({
  title: "", family: "logical", totalSeconds: 0, perItemSeconds: 0, minDifficulty: 1, maxDifficulty: 3,
});

export default function Compose() {
  const token = useApiToken();
  const [id, setId] = useState("");
  const [title, setTitle] = useState("");
  const [policy, setPolicy] = useState<DeliveryPolicy>("fixed-increasing");
  const [totalSeconds, setTotalSeconds] = useState(0);
  const [perItemSeconds, setPerItemSeconds] = useState(0);
  const [sections, setSections] = useState<SectionForm[]>([emptySection()]);
  const [invalid, setInvalid] = useState("");

  const patchSection = (i: number, patch: Partial<SectionForm>) =>
    setSections((prev) => prev.map((s, j) => (j === i ? { ...s, ...patch } : s)));

  // numChange coerces a number field, dropping the transient NaN a number input
  // can emit mid-edit so a request never serializes NaN → null on the wire.
  const numChange = (set: (n: number) => void) => (e: ChangeEvent<HTMLInputElement>) => {
    const n = Number(e.target.value);
    if (!Number.isNaN(n)) set(n);
  };

  const compose = useMutation({
    mutationFn: () =>
      api.compose(token, {
        id, title, policy, totalSeconds, perItemSeconds,
        sections: sections.map((s) => ({
          title: s.title, family: s.family,
          totalSeconds: s.totalSeconds, perItemSeconds: s.perItemSeconds,
          minDifficulty: s.minDifficulty, maxDifficulty: s.maxDifficulty,
        })),
      }),
  });

  const submit = () => {
    // Mirror the server invariant (domain/testset: an adaptive section must
    // offer ≥2 difficulty bands), catching the single-band case before the 400.
    if (policy === "adaptive" && sections.some((s) => s.maxDifficulty <= s.minDifficulty)) {
      setInvalid("Adaptive tests need each section to span at least two difficulty bands (max > min).");
      return;
    }
    setInvalid("");
    compose.mutate();
  };

  const numField = (label: string, value: number, set: (n: number) => void) => (
    <label className="block text-sm">{label}
      <input type="number" value={value} onChange={numChange(set)}
        className="mt-1 w-full rounded border px-2 py-1" />
    </label>
  );

  return (
    <div className="max-w-2xl">
      <h1 className="mb-4 text-xl font-semibold">Compose a test</h1>
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); submit(); }}>
        <label className="block text-sm">Test ID
          <input value={id} onChange={(e) => setId(e.target.value)} className="mt-1 w-full rounded border px-2 py-1" />
        </label>
        <label className="block text-sm">Title
          <input value={title} onChange={(e) => setTitle(e.target.value)} className="mt-1 w-full rounded border px-2 py-1" />
        </label>
        <label className="block text-sm">Policy
          <select value={policy} onChange={(e) => setPolicy(e.target.value as DeliveryPolicy)}
            className="mt-1 w-full rounded border px-2 py-1">
            <option value="fixed-increasing">fixed-increasing</option>
            <option value="adaptive">adaptive</option>
          </select>
        </label>
        <div className="grid grid-cols-2 gap-3">
          {numField("Total seconds (0 = untimed)", totalSeconds, setTotalSeconds)}
          {numField("Per-item seconds (0 = untimed)", perItemSeconds, setPerItemSeconds)}
        </div>

        <fieldset className="space-y-3">
          <legend className="text-sm font-medium">Sections</legend>
          {sections.map((s, i) => (
            <div key={i} className="space-y-2 rounded border p-3">
              <div className="flex items-center gap-2">
                <input
                  placeholder="Section title"
                  aria-label={`section ${i + 1} title`}
                  value={s.title}
                  onChange={(e) => patchSection(i, { title: e.target.value })}
                  className="flex-1 rounded border px-2 py-1 text-sm"
                />
                <select
                  aria-label={`section ${i + 1} family`}
                  value={s.family}
                  onChange={(e) => patchSection(i, { family: e.target.value })}
                  className="rounded border px-2 py-1 text-sm"
                >
                  {families.map((f) => <option key={f} value={f}>{f}</option>)}
                </select>
                {sections.length > 1 && (
                  <button type="button" onClick={() => setSections(sections.filter((_, j) => j !== i))}
                    className="rounded border px-2 py-1 text-sm text-red-600">Remove</button>
                )}
              </div>
              <div className="grid grid-cols-4 gap-2 text-sm">
                <input type="number" aria-label={`section ${i + 1} total seconds`} value={s.totalSeconds}
                  onChange={numChange((n) => patchSection(i, { totalSeconds: n }))}
                  className="rounded border px-2 py-1" placeholder="total s" />
                <input type="number" aria-label={`section ${i + 1} per-item seconds`} value={s.perItemSeconds}
                  onChange={numChange((n) => patchSection(i, { perItemSeconds: n }))}
                  className="rounded border px-2 py-1" placeholder="per-item s" />
                <input type="number" aria-label={`section ${i + 1} min difficulty`} value={s.minDifficulty}
                  onChange={numChange((n) => patchSection(i, { minDifficulty: n }))}
                  className="rounded border px-2 py-1" placeholder="min diff" />
                <input type="number" aria-label={`section ${i + 1} max difficulty`} value={s.maxDifficulty}
                  onChange={numChange((n) => patchSection(i, { maxDifficulty: n }))}
                  className="rounded border px-2 py-1" placeholder="max diff" />
              </div>
            </div>
          ))}
          <button type="button" onClick={() => setSections([...sections, emptySection()])}
            className="rounded border px-3 py-1 text-sm">Add section</button>
        </fieldset>

        <button type="submit" disabled={compose.isPending}
          className="rounded bg-slate-800 px-4 py-2 text-white disabled:opacity-50">
          {compose.isPending ? "Composing…" : "Compose test"}
        </button>
      </form>

      {invalid && <p className="mt-3 text-sm text-red-600">{invalid}</p>}
      {compose.isError && <p className="mt-3 text-sm text-red-600">Compose failed (check the id is unique and the bank has matching items).</p>}
      {compose.isSuccess && compose.data && (
        <p className="mt-3 text-sm text-green-700">
          Composed <Link className="font-mono underline" to={`/tests/${compose.data.ID}`}>{compose.data.ID}</Link>.
        </p>
      )}
    </div>
  );
}
