import { Link } from "react-router-dom";
import { useItems, useSources, useTests } from "../api/hooks";

function StatCard({ label, value, to }: { label: string; value: number | string; to: string }) {
  return (
    <Link to={to} className="rounded-lg border p-6 hover:bg-slate-50">
      <div className="text-3xl font-semibold">{value}</div>
      <div className="text-sm text-slate-500">{label}</div>
    </Link>
  );
}

export default function Dashboard() {
  const sources = useSources("?limit=1");
  const items = useItems("?limit=1");
  const tests = useTests("?limit=1");
  return (
    <div>
      <h1 className="mb-6 text-xl font-semibold">Dashboard</h1>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
        <StatCard label="Sources" value={sources.data?.total ?? "…"} to="/sources" />
        <StatCard label="Bank items" value={items.data?.total ?? "…"} to="/items" />
        <StatCard label="Tests" value={tests.data?.total ?? "…"} to="/tests" />
      </div>
    </div>
  );
}
