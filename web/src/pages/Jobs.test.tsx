import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "../auth/AuthContext";
import Jobs from "./Jobs";

const jobs = [
  {
    id: "j1", kind: "ingest-llm", sourceId: "src-a", state: "running",
    createdAt: "2026-07-09T12:00:00Z", startedAt: "2026-07-09T12:00:00Z", endedAt: "0001-01-01T00:00:00Z",
  },
  {
    id: "j2", kind: "ingest", sourceId: "src-b", state: "done",
    report: { SourceID: "src-b", Fetched: 40, Normalized: 40, Saved: 38, Skipped: 2, Note: "" },
    createdAt: "2026-07-09T11:00:00Z", startedAt: "2026-07-09T11:00:00Z", endedAt: "2026-07-09T11:01:00Z",
  },
];

function stubFetch() {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        new Response(
          JSON.stringify(
            url.includes("whoami")
              ? { role: "operator", mode: "none" }
              : { items: jobs, total: 2, limit: 50, offset: 0 },
          ),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    ),
  );
}

function renderJobs() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <AuthProvider>
        <Jobs />
      </AuthProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("Jobs", () => {
  it("renders each job row with its state and source", async () => {
    stubFetch();
    renderJobs();
    await waitFor(() => expect(screen.getByText("running")).toBeInTheDocument());
    expect(screen.getByText("done")).toBeInTheDocument();
    expect(screen.getByText("src-a")).toBeInTheDocument();
    expect(screen.getByText("src-b")).toBeInTheDocument();
    // The done job's report.Saved renders; the running job has no report yet.
    expect(screen.getByText("38")).toBeInTheDocument();
  });
});
