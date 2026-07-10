import { describe, expect, it, vi, afterEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createElement, type ReactNode } from "react";
import { useTakeSession } from "./useTakeSession";

function jsonResponse(body: unknown, headers: Record<string, string> = {}) {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json", ...headers } });
}

// useTakeSession calls useQuery, so a QueryClientProvider must wrap the hook.
function wrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => createElement(QueryClientProvider, { client }, children);
}

afterEach(() => vi.restoreAllMocks());

describe("useTakeSession", () => {
  it("previews then starts a session, moving to in-test", async () => {
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url.includes("/invites/preview")) return Promise.resolve(jsonResponse({ testId: "t1", title: "Quiz", sections: [], itemCount: 3 }));
      if (url.includes("/invites/start")) {
        return Promise.resolve(jsonResponse({
          Session: { ID: "s1", Presented: { ItemID: "i1" }, StartedAt: "2030-01-01T00:00:00Z", Timing: { Total: 0, PerItem: 0 } },
          Item: { ID: "i1", AnswerFormat: "multiple-choice", Options: [{ ID: "a", Text: "A" }], Stimulus: [], AnswerKey: {} },
          Deadline: "0001-01-01T00:00:00Z",
          SessionToken: "ts.tok",
        }));
      }
      void init;
      return Promise.resolve(jsonResponse({}));
    });
    vi.stubGlobal("fetch", fetchMock);

    const { result } = renderHook(() => useTakeSession("ti.invite"), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.preview?.title).toBe("Quiz"));
    expect(result.current.phase).toBe("preview");

    await act(async () => { await result.current.start(); });
    expect(result.current.phase).toBe("in-test");
    expect(result.current.delivery?.Session.Presented.ItemID).toBe("i1");
  });
});
