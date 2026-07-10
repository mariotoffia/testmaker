import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, act, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import Take from "./Take";

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

const startBody = {
  Session: { ID: "s1", Presented: { ItemID: "i1" }, StartedAt: "2030-01-01T00:00:00Z", Timing: { Total: 0, PerItem: 30_000_000_000 }, State: "in-progress" },
  Item: {
    ID: "i1", AnswerFormat: "multiple-choice",
    Options: [{ ID: "a", Text: "Alpha" }, { ID: "b", Text: "Beta" }],
    Stimulus: [], AnswerKey: {}, Difficulty: { Band: 1 },
  },
  Deadline: "2030-01-01T00:00:30Z", // per-item deadline 30s out
  SessionToken: "ts.tok",
};

const doneBody = {
  Session: { ID: "s1", Presented: { ItemID: "" }, StartedAt: "2030-01-01T00:00:00Z", Timing: { Total: 0, PerItem: 0 }, State: "completed" },
  Item: null,
  Deadline: "0001-01-01T00:00:00Z",
};

const scoreBody = { Raw: 0, Max: 1, Normed: false, Percentile: 0, ScaledIQ: 0, Band: "", Speed: { Total: 0, Mean: 0, CorrectPerMinute: 0 }, Items: [] };

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
});

describe("Take auto-submit", () => {
  it("auto-submits the current answer when the per-item deadline lapses", async () => {
    const calls: { url: string; body?: unknown }[] = [];
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      const body = init?.body ? JSON.parse(init.body as string) : undefined;
      calls.push({ url, body });
      if (url.includes("/invites/preview")) return Promise.resolve(jsonResponse({ testId: "t1", title: "Quiz", itemCount: 1, sections: [] }));
      if (url.includes("/invites/start")) return Promise.resolve(jsonResponse(startBody));
      if (url.includes("/answers")) return Promise.resolve(jsonResponse(doneBody));
      if (url.includes("/complete")) return Promise.resolve(jsonResponse({}));
      if (url.includes("/score")) return Promise.resolve(jsonResponse(scoreBody));
      return Promise.resolve(jsonResponse({}));
    });
    vi.stubGlobal("fetch", fetchMock);

    renderTake();
    await act(async () => { await vi.advanceTimersByTimeAsync(0); }); // resolve preview query
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: /start/i })); });
    await act(async () => { await vi.advanceTimersByTimeAsync(0); }); // resolve start()

    // Taker selects option "Beta" (option b) but never presses submit.
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: /Beta/ })); });

    // Per-item deadline lapses → auto-submit must fire with the SELECTED answer.
    await act(async () => { await vi.advanceTimersByTimeAsync(31_000); });
    await act(async () => { await vi.advanceTimersByTimeAsync(0); }); // resolve answer+complete
    await act(async () => { await vi.advanceTimersByTimeAsync(0); }); // resolve score query

    const answerCall = calls.find((c) => c.url.includes("/answers"));
    expect(answerCall).toBeTruthy();
    expect((answerCall!.body as { optionId?: string }).optionId).toBe("b"); // selection, not empty
    expect(screen.getByText(/your result/i)).toBeInTheDocument();
  });
});
