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
  it("shows saved counts and refreshes the item count after a synchronous ingest", async () => {
    // The source starts with 5 bank items; the ingest saves 3, and the next
    // source GET reports 8 — proving the mutation invalidated the source query.
    let itemCount = 5;
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) => {
        let body: unknown;
        if (url.includes("whoami")) body = { role: "operator", mode: "none" };
        else if (url.endsWith("/ingest")) {
          itemCount = 8;
          body = { SourceID: "s1", Fetched: 5, Normalized: 5, Saved: 3, Skipped: 2, Note: "" };
        } else body = { ...source, ItemCount: itemCount };
        return Promise.resolve(
          new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } }),
        );
      }),
    );
    renderDetail();
    await waitFor(() => expect(screen.getByText("Src One")).toBeInTheDocument());
    expect(screen.getByText("5")).toBeInTheDocument(); // initial Bank items count
    await userEvent.click(screen.getByRole("button", { name: /^ingest$/i }));
    await waitFor(() => expect(screen.getByText(/Saved 3 of 5/)).toBeInTheDocument());
    // invalidation refetched the source, so the Bank items figure updated 5 → 8.
    await waitFor(() => expect(screen.getByText("8")).toBeInTheDocument());
  });

  it("surfaces the server's reason when ingest is unsupported for this source", async () => {
    // Most catalogue sources have no normalizer wired; the server 501s with a
    // precise reason and the UI must show it, not a generic "Ingest failed."
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) => {
        if (url.includes("whoami"))
          return Promise.resolve(new Response(JSON.stringify({ role: "operator", mode: "none" }), { status: 200 }));
        if (url.endsWith("/ingest"))
          return Promise.resolve(
            new Response(
              JSON.stringify({ code: "ingest.no_normalizer", error: 'no normalizer registered for source "s1"' }),
              { status: 501, headers: { "Content-Type": "application/json" } },
            ),
          );
        return Promise.resolve(new Response(JSON.stringify(source), { status: 200 }));
      }),
    );
    renderDetail();
    await waitFor(() => expect(screen.getByText("Src One")).toBeInTheDocument());
    await userEvent.click(screen.getByRole("button", { name: /^ingest$/i }));
    await waitFor(() =>
      expect(screen.getByText(/no normalizer registered for source "s1"/)).toBeInTheDocument(),
    );
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
