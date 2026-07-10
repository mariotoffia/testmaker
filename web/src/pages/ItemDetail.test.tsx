import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "../auth/AuthContext";
import ItemDetail from "./ItemDetail";

const acme = {
  ID: "acme-iq", Name: "Acme IQ", Provider: "Acme Corp", Category: "iq",
  Families: ["logical"], TestTypes: ["A2"],
  URLs: ["https://acme.example/iq-test/", "https://acme.example/data.zip"],
  License: { Category: "public-domain", Detail: "", Redistributable: "yes" },
  Extraction: { Method: "http-json", Auth: "", ItemsAs: "", Notes: "" },
  ItemCount: 5,
};

const mkItem = (origin: string, sourceID: string) => ({
  ID: "it1",
  Provenance: { SourceID: sourceID, Origin: origin, Redistributable: "yes" },
  TestType: "A2", Family: "logical", AnswerFormat: "multiple-choice",
  Stimulus: [{ Text: "Which word means the same as large?", MediaKind: "", MediaRef: "" }],
  Options: [{ ID: "a", Text: "tiny", MediaKind: "", MediaRef: "" }, { ID: "b", Text: "big", MediaKind: "", MediaRef: "" }],
  AnswerKey: { OptionID: "b", Numeric: 0, Verdict: "", Tolerance: 0 },
  Explanation: "big = large", Difficulty: { Band: 2 },
});

function stub(item: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockImplementation((url: string) => {
    const body = url.includes("whoami") ? { role: "operator", mode: "none" }
      : url.includes("/api/sources/") ? acme
      : item;
    return Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } }));
  }));
}

function renderDetail() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <AuthProvider>
        <MemoryRouter initialEntries={["/items/it1"]}>
          <Routes><Route path="/items/:id" element={<ItemDetail />} /></Routes>
        </MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => { localStorage.clear(); vi.restoreAllMocks(); });

describe("ItemDetail attribution", () => {
  it("shows import + author links for a fetched item", async () => {
    stub(mkItem("fetched", "acme-iq"));
    renderDetail();
    await waitFor(() => expect(screen.getByText(/Which word means the same/)).toBeInTheDocument());

    expect(await screen.findByText(/Imported from/)).toBeInTheDocument();
    const imported = screen.getByRole("link", { name: /Acme IQ/ });
    expect(imported).toHaveAttribute("href", "https://acme.example/iq-test/");
    expect(imported).toHaveAttribute("target", "_blank");

    expect(screen.getByText(/More tests from/)).toBeInTheDocument();
    const more = screen.getByRole("link", { name: /Acme Corp/ });
    expect(more).toHaveAttribute("href", "https://acme.example");
  });

  it("shows no attribution for a generated item", async () => {
    stub(mkItem("generated", "rulegen"));
    renderDetail();
    await waitFor(() => expect(screen.getByText(/Which word means the same/)).toBeInTheDocument());
    expect(screen.queryByText(/Imported from/)).toBeNull();
    expect(screen.queryByText(/More tests from/)).toBeNull();
  });
});
