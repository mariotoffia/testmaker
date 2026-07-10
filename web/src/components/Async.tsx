import type { ReactNode } from "react";
import { ApiError } from "../api/client";

// Async renders the standard loading / error / ready states for a react-query
// result so pages don't each reinvent them. A 401/403 surfaces as an auth hint.
export function Async<T>({
  query,
  children,
}: {
  query: { isLoading: boolean; error: unknown; data: T | undefined };
  children: (data: T) => ReactNode;
}) {
  if (query.isLoading) return <p className="text-slate-500">Loading…</p>;
  if (query.error) {
    const e = query.error;
    const msg = e instanceof ApiError ? `${e.status} ${e.message}` : "Something went wrong";
    return <p className="text-red-600">{msg}</p>;
  }
  if (query.data === undefined) return null;
  return <>{children(query.data)}</>;
}
