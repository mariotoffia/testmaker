import type { ItemSnapshot } from "../api/types";
import type { Answer } from "../player/answer";
import { ItemView } from "./ItemView";
import { AnswerControl } from "../player/AnswerControl";

// PlayerItem composes the taker-facing stem (ItemView with showKey={false} and
// showOptions={false} — the server has stripped the answer key, and AnswerControl
// renders the options interactively, so ItemView shows the stimulus only) with
// the format-appropriate AnswerControl and the submit button.
export function PlayerItem({
  item, value, onChange, onSubmit, busy,
}: {
  item: ItemSnapshot; value: Answer; onChange: (a: Answer) => void; onSubmit: () => void; busy: boolean;
}) {
  return (
    <div className="space-y-6">
      <ItemView item={item} showKey={false} showOptions={false} />
      <AnswerControl item={item} value={value} onChange={onChange} onSubmit={onSubmit} />
      <button onClick={onSubmit} disabled={busy} className="rounded bg-slate-800 px-5 py-2 text-white disabled:opacity-50">
        {busy ? "Submitting…" : "Submit (Enter)"}
      </button>
    </div>
  );
}
