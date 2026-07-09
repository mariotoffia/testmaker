import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, apiFetch, serverSkewMs } from "./client";

function mockFetch(status: number, body: unknown, headers: Record<string, string> = {}) {
  return vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json", ...headers },
    }),
  );
}

afterEach(() => vi.restoreAllMocks());

describe("apiFetch", () => {
  it("returns the decoded body on 2xx", async () => {
    vi.stubGlobal("fetch", mockFetch(200, { items: [{ ID: "a" }], total: 1, limit: 50, offset: 0 }));
    const page = await apiFetch<{ items: { ID: string }[]; total: number }>("/api/items");
    expect(page.total).toBe(1);
    expect(page.items[0].ID).toBe("a");
  });

  it("throws ApiError carrying code + status on non-2xx", async () => {
    vi.stubGlobal("fetch", mockFetch(404, { error: "item not found", code: "item.unknown", class: "not_found" }));
    await expect(apiFetch("/api/items/x")).rejects.toMatchObject({
      status: 404,
      code: "item.unknown",
    } satisfies Partial<ApiError>);
  });

  it("sends the bearer token when provided", async () => {
    const f = mockFetch(200, { role: "operator" });
    vi.stubGlobal("fetch", f);
    await apiFetch("/api/auth/whoami", { token: "OP" });
    const init = f.mock.calls[0][1] as RequestInit;
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer OP");
  });

  it("captures server clock skew from the Date header", async () => {
    const serverNow = new Date("2030-01-01T00:00:10Z"); // 10s ahead of a fake local clock
    vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
    vi.stubGlobal("fetch", mockFetch(200, {}, { Date: serverNow.toUTCString() }));
    await apiFetch("/api");
    expect(serverSkewMs()).toBeGreaterThanOrEqual(9000);
    vi.useRealTimers();
  });
});
