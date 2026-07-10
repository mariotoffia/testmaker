import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, act, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import Take from "./Take";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// An untimed session: zero-time deadline, zero total → both countdowns null.
const untimedStart = {
  Session: { ID: "s1", Presented: { ItemID: "i1" }, StartedAt: "2030-01-01T00:00:00Z", Timing: { Total: 0, PerItem: 0 }, State: "in-progress" },
  Item: {
    ID: "i1", AnswerFormat: "multiple-choice",
    Options: [{ ID: "a", Text: "Alpha" }, { ID: "b", Text: "Beta" }],
    Stimulus: [], AnswerKey: {}, Difficulty: { Band: 1 },
  },
  Deadline: "0001-01-01T00:00:00Z",
  SessionToken: "ts.tok",
};

function renderTake() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <Take />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
  window.location.hash = "#ti.invite";
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  window.location.hash = "";
  sessionStorage.clear();
});

describe("Take edge states", () => {
  it("renders a clear notice (not a crash) for an expired/invalid invite", async () => {
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(jsonResponse({ code: "unauthorized", error: "bad token" }, 401))));
    renderTake();
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(screen.getByText(/no longer valid/i)).toBeInTheDocument();
  });

  it("keeps the current item and warns on a 409 conflict from answer", async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url.includes("/invites/preview")) return Promise.resolve(jsonResponse({ testId: "t1", title: "Quiz", itemCount: 1, sections: [] }));
      if (url.includes("/invites/start")) return Promise.resolve(jsonResponse(untimedStart));
      if (url.includes("/answers")) return Promise.resolve(jsonResponse({ code: "conflict", error: "version" }, 409));
      return Promise.resolve(jsonResponse({}));
    });
    vi.stubGlobal("fetch", fetchMock);
    renderTake();
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: /start/i })); });
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: /Beta/ })); });
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: /submit/i })); });
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });

    expect(screen.getByText(/another tab or window/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Beta/ })).toBeInTheDocument(); // still on the same item
  });

  it("renders no countdown and never auto-submits for an untimed test", async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url.includes("/invites/preview")) return Promise.resolve(jsonResponse({ testId: "t1", title: "Quiz", itemCount: 1, sections: [] }));
      if (url.includes("/invites/start")) return Promise.resolve(jsonResponse(untimedStart));
      return Promise.resolve(jsonResponse({}));
    });
    vi.stubGlobal("fetch", fetchMock);
    renderTake();
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: /start/i })); });
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });

    expect(screen.queryByText(/This item/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Total/)).not.toBeInTheDocument();

    await act(async () => { await vi.advanceTimersByTimeAsync(120_000); });
    expect(fetchMock.mock.calls.some((c) => String(c[0]).includes("/answers"))).toBe(false);
  });
});
