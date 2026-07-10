import type { ItemSnapshot } from "../api/types";
import { MediaRenderer } from "./MediaRenderer";

// ItemView renders an item's stem and options. showKey highlights the correct
// option — operator preview only; the player passes showKey={false} and the
// server has already stripped the key from a taker's presented item anyway.
// showOptions={false} renders the stimulus alone: the player owns option display
// via AnswerControl, so rendering options here too would duplicate every choice.
export function ItemView({ item, showKey, showOptions = true }: { item: ItemSnapshot; showKey: boolean; showOptions?: boolean }) {
  return (
    <div className="space-y-4">
      <div className="space-y-2">
        {(item.Stimulus ?? []).map((p, i) => (
          <div key={i}><MediaRenderer part={p} /></div>
        ))}
      </div>
      {showOptions && item.AnswerFormat === "multiple-choice" && (
        <ul className="space-y-1">
          {(item.Options ?? []).map((o) => {
            const correct = showKey && o.ID === item.AnswerKey.OptionID;
            return (
              <li key={o.ID} className={`rounded border p-2 ${correct ? "border-green-500 bg-green-50" : ""}`}>
                <span className="mr-2 font-mono text-xs text-slate-500">{o.ID}</span>
                <MediaRenderer part={o} />
              </li>
            );
          })}
        </ul>
      )}
      {showKey && item.Explanation && (
        <p className="rounded bg-slate-50 p-2 text-sm text-slate-600">{item.Explanation}</p>
      )}
    </div>
  );
}
