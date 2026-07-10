import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSources, useApiToken } from "../api/hooks";
import { Async } from "../components/Async";
import { api } from "../api/client";

export default function Sources() {
  const token = useApiToken();
  const qc = useQueryClient();
  const [family, setFamily] = useState("");
  const q = family ? `?family=${family}` : "";
  const sources = useSources(q);
  const sync = useMutation({
    mutationFn: () => api.syncCatalog(token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sources"] }),
  });
  const upload = useMutation({
    mutationFn: (json: string) => api.uploadCatalog(token, json),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sources"] }),
  });

  return (
    <div>
      <div className="mb-4 flex items-center gap-2">
        <h1 className="text-xl font-semibold">Sources</h1>
        <button
          onClick={() => sync.mutate()}
          disabled={sync.isPending}
          className="ml-auto rounded border px-3 py-1 text-sm disabled:opacity-50"
        >
          {sync.isPending ? "Syncing…" : "Sync catalogue"}
        </button>
        <label className="cursor-pointer rounded border px-3 py-1 text-sm">
          {upload.isPending ? "Uploading…" : "Upload JSON"}
          <input
            type="file"
            accept="application/json"
            hidden
            onChange={async (e) => {
              const f = e.target.files?.[0];
              if (f) upload.mutate(await f.text());
              e.target.value = "";
            }}
          />
        </label>
      </div>
      <select
        value={family}
        onChange={(e) => setFamily(e.target.value)}
        className="mb-3 rounded border px-2 py-1 text-sm"
        aria-label="filter by family"
      >
        <option value="">all families</option>
        {["logical", "numerical", "verbal", "spatial", "speed"].map((f) => (
          <option key={f} value={f}>{f}</option>
        ))}
      </select>
      {upload.isError && <p className="mb-2 text-sm text-red-600">Upload rejected (invalid catalogue).</p>}
      <Async query={sources}>
        {(page) =>
          page.items.length === 0 ? (
            <p className="text-slate-500">No sources yet. Sync the catalogue or upload a JSON descriptor.</p>
          ) : (
            <table className="w-full text-sm">
              <thead className="text-left text-slate-500">
                <tr><th className="py-1">Name</th><th>Provider</th><th>Redistributable</th><th>Items</th></tr>
              </thead>
              <tbody>
                {page.items.map((s) => (
                  <tr key={s.ID} className="border-t">
                    <td className="py-1"><Link className="text-blue-700 hover:underline" to={`/sources/${s.ID}`}>{s.Name}</Link></td>
                    <td>{s.Provider}</td>
                    <td>{s.License.Redistributable}</td>
                    <td>{s.ItemCount}</td>
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
