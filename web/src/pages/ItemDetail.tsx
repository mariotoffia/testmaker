import { Link, useParams } from "react-router-dom";
import { useItem, useSource } from "../api/hooks";
import { primaryUrl, siteRoot } from "../api/attribution";
import { Async } from "../components/Async";
import { ItemView } from "../components/ItemView";
import type { ItemSnapshot } from "../api/types";

// ItemAttribution shows where a fetched item came from and links back to the
// author's site. It resolves the source via its own hook (so it runs inside the
// item's Async render) and renders nothing for generated items or sources with
// no linkable URL.
function ItemAttribution({ item }: { item: ItemSnapshot }) {
  const fetched = item.Provenance.Origin === "fetched";
  const src = useSource(fetched ? item.Provenance.SourceID : "");
  const s = src.data;
  if (!fetched || !s) return null;
  const from = primaryUrl(s.URLs);
  const site = siteRoot((s.URLs ?? [])[0]);
  if (!from && !site) return null;
  return (
    <div className="space-y-1 border-t pt-3 text-sm text-slate-600">
      {from && (
        <div>
          Imported from{" "}
          <a href={from} target="_blank" rel="noopener noreferrer" className="text-blue-700 hover:underline">{s.Name} ↗</a>
        </div>
      )}
      {site && (
        <div>
          More tests from{" "}
          <a href={site} target="_blank" rel="noopener noreferrer" className="text-blue-700 hover:underline">{s.Provider} ↗</a>
        </div>
      )}
    </div>
  );
}

// ItemDetail is the operator's full-item preview: it shows the answer key and
// explanation (showKey), which the player view deliberately withholds.
export default function ItemDetail() {
  const { id = "" } = useParams();
  const item = useItem(id);
  return (
    <div className="max-w-2xl">
      <Link to="/items" className="text-sm text-blue-700 hover:underline">← Item bank</Link>
      <Async query={item}>
        {(it) => (
          <div className="mt-3 space-y-4">
            <div className="flex flex-wrap items-center gap-2 text-sm text-slate-500">
              <span className="font-mono">{it.ID}</span>
              <span>· {it.TestType}</span>
              <span>· {it.Family}</span>
              <span>· difficulty {it.Difficulty.Band}</span>
            </div>
            <ItemView item={it} showKey />
            <ItemAttribution item={it} />
          </div>
        )}
      </Async>
    </div>
  );
}
