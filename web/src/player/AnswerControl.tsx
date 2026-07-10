import { useEffect } from "react";
import type { ItemSnapshot } from "../api/types";
import type { Answer } from "./answer";
import { MediaRenderer } from "../components/MediaRenderer";

// AnswerControl renders the input for an item's answer format and wires
// keyboard-first entry — speeded tests live or die on input latency (ADR-0005):
// digits 1–6 pick a multiple-choice option, Enter submits, T/F/C pick a verdict.
export function AnswerControl({
  item, value, onChange, onSubmit,
}: {
  item: ItemSnapshot;
  value: Answer;
  onChange: (a: Answer) => void;
  onSubmit: () => void;
}) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Enter") { onSubmit(); return; }
      if (item.AnswerFormat === "multiple-choice") {
        const n = Number(e.key);
        const opts = item.Options ?? [];
        if (n >= 1 && n <= opts.length) onChange({ itemId: item.ID, optionId: opts[n - 1].ID });
      } else if (item.AnswerFormat === "true-false-cannotsay") {
        const v = { t: "true", f: "false", c: "cannot-say" }[e.key.toLowerCase()];
        if (v) onChange({ itemId: item.ID, verdict: v });
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [item, onChange, onSubmit]);

  if (item.AnswerFormat === "multiple-choice") {
    return (
      <ul className="space-y-2">
        {(item.Options ?? []).map((o, i) => (
          <li key={o.ID}>
            <button
              onClick={() => onChange({ itemId: item.ID, optionId: o.ID })}
              className={`flex w-full items-center gap-2 rounded border p-3 text-left ${
                value.optionId === o.ID ? "border-slate-800 bg-slate-100" : "hover:bg-slate-50"
              }`}
            >
              <kbd className="rounded bg-slate-200 px-1.5 text-xs">{i + 1}</kbd>
              <MediaRenderer part={o} />
            </button>
          </li>
        ))}
      </ul>
    );
  }
  if (item.AnswerFormat === "open-numeric") {
    return (
      <input
        type="number"
        autoFocus
        value={value.numeric ?? ""}
        onChange={(e) => onChange({ itemId: item.ID, numeric: Number(e.target.value) })}
        className="w-40 rounded border px-3 py-2"
        aria-label="numeric answer"
      />
    );
  }
  return (
    <div className="flex gap-2">
      {[["true", "True (T)"], ["false", "False (F)"], ["cannot-say", "Cannot say (C)"]].map(([v, label]) => (
        <button
          key={v}
          onClick={() => onChange({ itemId: item.ID, verdict: v })}
          className={`rounded border px-4 py-2 ${value.verdict === v ? "border-slate-800 bg-slate-100" : "hover:bg-slate-50"}`}
        >
          {label}
        </button>
      ))}
    </div>
  );
}
