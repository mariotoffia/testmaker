import { useTakeSession } from "../player/useTakeSession";

// Take is the public player route (/take). The taker's authority is the
// invite/session capability token in the URL fragment (#…), never the operator
// auth context (ADR-0005). It renders one view per session phase.
export default function Take() {
  const invite = window.location.hash.slice(1);
  const s = useTakeSession(invite);

  if (!invite) return <p className="p-8">This link is missing its invite token.</p>;

  if (s.phase === "preview") {
    if (s.previewError) return <p className="p-8">Loading…</p>;
    const p = s.preview;
    if (!p) return <p className="p-8">Loading…</p>;
    return (
      <div className="mx-auto max-w-2xl p-6">
        <h1 className="text-2xl font-semibold">{p.title}</h1>
        <p className="mt-2 text-slate-600">{p.itemCount} items{p.totalSeconds ? ` · ${Math.round(p.totalSeconds / 60)} min` : ""}</p>
        <button onClick={() => s.start()} className="mt-6 rounded bg-slate-800 px-5 py-2 text-white">
          Start
        </button>
      </div>
    );
  }

  // in-test and complete views are wired in Tasks 33–34.
  return <div className="mx-auto max-w-2xl p-6" />;
}
