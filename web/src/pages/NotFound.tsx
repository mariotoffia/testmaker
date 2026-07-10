import { Link } from "react-router-dom";

export default function NotFound() {
  return (
    <div className="p-8">
      <h1 className="text-xl font-semibold">404 — page not found</h1>
      <p className="mt-2 text-sm text-slate-500">That route doesn’t exist in the console.</p>
      <Link to="/" className="mt-4 inline-block text-sm text-blue-700 hover:underline">← Back to the dashboard</Link>
    </div>
  );
}
