// Countdown renders mm:ss, turning red under 10s. An untimed slot (null) shows
// nothing.
export function Countdown({ ms, label }: { ms: number | null; label: string }) {
  if (ms === null) return null;
  const total = Math.ceil(ms / 1000);
  const mm = String(Math.floor(total / 60)).padStart(2, "0");
  const ss = String(total % 60).padStart(2, "0");
  return (
    <span className={`font-mono text-sm ${total <= 10 ? "text-red-600" : "text-slate-600"}`}>
      {label} {mm}:{ss}
    </span>
  );
}
