import { useJobs } from "../api/hooks";
import { Async } from "../components/Async";
import { JobBadge } from "../components/JobBadge";

export default function Jobs() {
  const jobs = useJobs(1500); // poll every 1.5s while the tab is open
  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold">Ingest jobs</h1>
      <Async query={jobs}>
        {(page) =>
          page.items.length === 0 ? (
            <p className="text-slate-500">No jobs yet. Trigger an ingest from a source.</p>
          ) : (
            <table className="w-full text-sm">
              <thead className="text-left text-slate-500">
                <tr><th className="py-1">Source</th><th>Kind</th><th>State</th><th>Saved</th></tr>
              </thead>
              <tbody>
                {page.items.map((j) => (
                  <tr key={j.id} className="border-t">
                    <td className="py-1">{j.sourceId}</td>
                    <td>{j.kind}</td>
                    <td><JobBadge state={j.state} /></td>
                    <td>{j.report?.Saved ?? (j.error ? "—" : "")}</td>
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
