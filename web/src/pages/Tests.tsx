import { Link } from "react-router-dom";
import { useTests } from "../api/hooks";
import { Async } from "../components/Async";

export default function Tests() {
  const tests = useTests();
  return (
    <div>
      <div className="mb-4 flex items-center gap-2">
        <h1 className="text-xl font-semibold">Tests</h1>
        <Link to="/compose" className="ml-auto rounded border px-3 py-1 text-sm">Compose a test</Link>
      </div>
      <Async query={tests}>
        {(page) =>
          page.items.length === 0 ? (
            <p className="text-slate-500">No tests yet. Compose one from the item bank.</p>
          ) : (
            <table className="w-full text-sm">
              <thead className="text-left text-slate-500">
                <tr><th className="py-1">ID</th><th>Title</th><th>Policy</th><th>Sections</th></tr>
              </thead>
              <tbody>
                {page.items.map((t) => (
                  <tr key={t.ID} className="border-t">
                    <td className="py-1"><Link className="font-mono text-blue-700 hover:underline" to={`/tests/${t.ID}`}>{t.ID}</Link></td>
                    <td>{t.Title}</td>
                    <td>{t.Policy}</td>
                    <td>{(t.Sections ?? []).length}</td>
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
