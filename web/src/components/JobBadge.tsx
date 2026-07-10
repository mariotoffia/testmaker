import type { Job } from "../api/types";

const styles: Record<Job["state"], string> = {
  queued: "bg-slate-200 text-slate-700",
  running: "bg-blue-200 text-blue-800",
  done: "bg-green-200 text-green-800",
  failed: "bg-red-200 text-red-800",
};

export function JobBadge({ state }: { state: Job["state"] }) {
  return <span className={`rounded px-2 py-0.5 text-xs font-medium ${styles[state]}`}>{state}</span>;
}
