import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ScoreView } from "./ScoreView";

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

function renderScore() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <ScoreView sid="s1" token="ts.tok" />
    </QueryClientProvider>,
  );
}

afterEach(() => vi.restoreAllMocks());

describe("ScoreView", () => {
  it("renders IQ, band, and per-item feedback for a normed score", async () => {
    const normed = {
      Raw: 8, Max: 10, Normed: true, Percentile: 82.4, ScaledIQ: 114, Band: "High Average",
      Speed: { Total: 300_000_000_000, Mean: 30_000_000_000, CorrectPerMinute: 1.6 },
      Items: [{ ItemID: "i1", Correct: false, Given: "a", CorrectAnswer: "b", Explanation: "B rotates 90°.", Elapsed: 0 }],
    };
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(jsonResponse(normed))));
    renderScore();

    await waitFor(() => expect(screen.getByText("114")).toBeInTheDocument()); // Scaled IQ
    expect(screen.getByText(/High Average/)).toBeInTheDocument();
    expect(screen.getByText(/B rotates 90/)).toBeInTheDocument();
    expect(screen.getByText("8/10")).toBeInTheDocument();
  });

  it("shows a raw-only note and no IQ for an unnormed score", async () => {
    const raw = {
      Raw: 5, Max: 10, Normed: false, Percentile: 0, ScaledIQ: 0, Band: "",
      Speed: { Total: 0, Mean: 0, CorrectPerMinute: 0 }, Items: [],
    };
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(jsonResponse(raw))));
    renderScore();

    await waitFor(() => expect(screen.getByText(/raw score only/i)).toBeInTheDocument());
    expect(screen.queryByText(/scaled iq/i)).not.toBeInTheDocument();
  });
});
