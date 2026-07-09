import type {
  Delivery, InvitePreview, Invite, IngestReport, ItemSnapshot, Job, Page, Score,
  SourceSnapshot, StartResponse, TestSnapshot, Whoami,
} from "./types";

// ApiError carries the transport status plus the server's error code/message so
// the UI can branch on 401/403/409 and show a safe message (C4).
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

let skewMs = 0;
// serverSkewMs is (server clock − local clock) in ms, refreshed from every
// response's Date header, so the player can render deadlines against the
// server's clock rather than a possibly-wrong local one (C10).
export function serverSkewMs(): number {
  return skewMs;
}

interface FetchOpts {
  method?: string;
  body?: unknown;
  token?: string;
  raw?: string; // raw text body (catalogue upload) — bypasses JSON.stringify
}

export async function apiFetch<T>(path: string, opts: FetchOpts = {}): Promise<T> {
  const headers: Record<string, string> = {};
  if (opts.token) headers.Authorization = `Bearer ${opts.token}`;
  let body: BodyInit | undefined;
  if (opts.raw !== undefined) {
    headers["Content-Type"] = "application/json";
    body = opts.raw;
  } else if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(opts.body);
  }
  const res = await fetch(path, { method: opts.method ?? (body ? "POST" : "GET"), headers, body });

  const date = res.headers.get("Date");
  if (date) skewMs = Date.parse(date) - Date.now();

  const text = await res.text();
  const parsed = text ? JSON.parse(text) : {};
  if (!res.ok) {
    throw new ApiError(res.status, parsed.code ?? "error", parsed.error ?? res.statusText);
  }
  return parsed as T;
}

type IngestSync = IngestReport;

// --- typed endpoint helpers (token threaded in by the auth context, Task 21) ---
export const api = {
  whoami: (token?: string) => apiFetch<Whoami>("/api/auth/whoami", { token }),

  listSources: (token: string, q = "") => apiFetch<Page<SourceSnapshot>>(`/api/sources${q}`, { token }),
  getSource: (token: string, id: string) => apiFetch<SourceSnapshot>(`/api/sources/${id}`, { token }),
  syncCatalog: (token: string) => apiFetch<{ synced: number }>("/api/catalog/sync", { token, method: "POST" }),
  uploadCatalog: (token: string, json: string) => apiFetch<{ synced: number }>("/api/catalog", { token, raw: json }),

  listItems: (token: string, q = "") => apiFetch<Page<ItemSnapshot>>(`/api/items${q}`, { token }),
  getItem: (token: string, id: string) => apiFetch<ItemSnapshot>(`/api/items/${id}`, { token }),
  generate: (token: string, body: object) => apiFetch<unknown>("/api/items/generate", { token, body }),

  ingest: (token: string, id: string, body: object) => apiFetch<Job | IngestSync>(`/api/sources/${id}/ingest`, { token, body }),
  ingestLLM: (token: string, id: string, body: object) => apiFetch<Job | IngestSync>(`/api/sources/${id}/ingest-llm`, { token, body }),
  listJobs: (token: string, q = "") => apiFetch<Page<Job>>(`/api/jobs${q}`, { token }),
  getJob: (token: string, id: string) => apiFetch<Job>(`/api/jobs/${id}`, { token }),

  listTests: (token: string, q = "") => apiFetch<Page<TestSnapshot>>(`/api/tests${q}`, { token }),
  getTest: (token: string, id: string) => apiFetch<TestSnapshot>(`/api/tests/${id}`, { token }),
  compose: (token: string, body: object) => apiFetch<TestSnapshot>("/api/tests", { token, body }),
  mintInvite: (token: string, id: string, body: object) => apiFetch<Invite>(`/api/tests/${id}/invites`, { token, body }),

  previewInvite: (invite: string) => apiFetch<InvitePreview>("/api/invites/preview", { token: invite }),
  startInvite: (invite: string) => apiFetch<StartResponse>("/api/invites/start", { token: invite, method: "POST" }),
  answer: (token: string, sid: string, body: object) => apiFetch<Delivery>(`/api/sessions/${sid}/answers`, { token, body }),
  complete: (token: string, sid: string) => apiFetch<unknown>(`/api/sessions/${sid}/complete`, { token, method: "POST" }),
  score: (token: string, sid: string) => apiFetch<Score>(`/api/sessions/${sid}/score`, { token }),
};
