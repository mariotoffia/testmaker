import { describe, expect, it, vi, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { AuthProvider } from "../auth/AuthContext";
import { useItems } from "./hooks";

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <AuthProvider>{children}</AuthProvider>
    </QueryClientProvider>
  );
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("useItems", () => {
  it("fetches a page of items", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) =>
        Promise.resolve(
          new Response(
            JSON.stringify(
              url.includes("whoami")
                ? { role: "operator", mode: "none" }
                : { items: [{ ID: "i1" }], total: 1, limit: 50, offset: 0 },
            ),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        ),
      ),
    );
    const { result } = renderHook(() => useItems(""), { wrapper });
    await waitFor(() => expect(result.current.data?.total).toBe(1));
    expect(result.current.data?.items[0].ID).toBe("i1");
  });
});
