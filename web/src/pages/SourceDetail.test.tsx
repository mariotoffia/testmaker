import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "../auth/AuthContext";
import SourceDetail from "./SourceDetail";

const source = {
  ID: "s1", Name: "Src One", Provider: "p", Category: "iq",
  Families: ["logical"], TestTypes: ["A2"],
  License: { Category: "public-domain", Detail: "", Redistributable: "yes" },
  Extraction: { Method: "http-json", Auth: "", ItemsAs: "", Notes: "" },
  ItemCount: 5,
};

// stub routes fetch by URL: whoami, the source GET, and a caller-supplied ingest
// response body (Job for async, IngestReport for sync).
function stubFetch(ingestBody: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation((url: string) => {
      const body = url.includes("whoami")
        ? { role: "operator", mode: "none" }
        : url.endsWith("/ingest")
          ? ingestBody
          : source;
      return Promise.resolve(
        new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } }),
      );
    }),
  );
}

function renderDetail() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <AuthProvider>
        <MemoryRouter initialEntries={["/sources/s1"]}>
          <Routes>
            <Route path="/sources/:id" element={<SourceDetail />} />
            <Route path="/jobs" element={<div>JOBS PAGE</div>} />
          </Routes>
        </MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("SourceDetail ingest", () => {
  it("shows saved counts after a synchronous ingest", async () => {
    stubFetch({ SourceID: "s1", Fetched: 5, Normalized: 5, Saved: 3, Skipped: 2, Note: "" });
    renderDetail();
    await waitFor(() => expect(screen.getByText("Src One")).toBeInTheDocument());
    await userEvent.click(screen.getByRole("button", { name: /^ingest$/i }));
    await waitFor(() => expect(screen.getByText(/Saved 3 of 5/)).toBeInTheDocument());
  });

  it("navigates to the jobs page after an async ingest", async () => {
    stubFetch({ id: "j1", kind: "ingest", sourceId: "s1", state: "queued", createdAt: "", startedAt: "", endedAt: "" });
    renderDetail();
    await waitFor(() => expect(screen.getByText("Src One")).toBeInTheDocument());
    await userEvent.click(screen.getByLabelText(/background job/i));
    await userEvent.click(screen.getByRole("button", { name: /^ingest$/i }));
    await waitFor(() => expect(screen.getByText("JOBS PAGE")).toBeInTheDocument());
  });
});
