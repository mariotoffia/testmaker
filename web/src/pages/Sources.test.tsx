import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "../auth/AuthContext";
import Sources from "./Sources";

const source = {
  ID: "s1",
  Name: "OpenPsychometrics VIQT",
  Provider: "openpsychometrics.org",
  Category: "iq",
  Families: ["logical"],
  TestTypes: ["A2"],
  License: { Category: "public-domain", Detail: "", Redistributable: "yes" },
  Extraction: { Method: "http-json", Auth: "", ItemsAs: "", Notes: "" },
  ItemCount: 40,
};

function stubFetch() {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        new Response(
          JSON.stringify(
            url.includes("whoami")
              ? { role: "operator", mode: "none" }
              : { items: [source], total: 1, limit: 50, offset: 0 },
          ),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    ),
  );
}

function renderSources() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <AuthProvider>
        <MemoryRouter>
          <Sources />
        </MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("Sources", () => {
  it("renders a source name in a table row", async () => {
    stubFetch();
    renderSources();
    await waitFor(() => expect(screen.getByText("OpenPsychometrics VIQT")).toBeInTheDocument());
    expect(screen.getByText("openpsychometrics.org")).toBeInTheDocument();
  });
});
