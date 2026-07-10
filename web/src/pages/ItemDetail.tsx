import { Link, useParams } from "react-router-dom";
import { useItem } from "../api/hooks";
import { Async } from "../components/Async";
import { ItemView } from "../components/ItemView";

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
          </div>
        )}
      </Async>
    </div>
  );
}
