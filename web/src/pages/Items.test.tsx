import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "../auth/AuthContext";
import Items from "./Items";

const mkItem = (id: string) => ({
  ID: id, TestType: "A2", Family: "logical", AnswerFormat: "multiple-choice",
  Difficulty: { Band: 2 },
});

// stubItems serves a mutable bank: GET returns the current list, DELETE removes
// the id and 204s — so a refetch after delete reflects the smaller bank.
function stubItems(initial: string[]) {
  const bank = new Map(initial.map((id) => [id, mkItem(id)]));
  const deleted: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation((url: string, opts?: RequestInit) => {
      if (url.includes("whoami"))
        return Promise.resolve(new Response(JSON.stringify({ role: "operator", mode: "none" }), { status: 200 }));
      if (opts?.method === "DELETE") {
        const id = url.split("/api/items/")[1];
        deleted.push(id);
        bank.delete(id);
        return Promise.resolve(new Response("", { status: 204 }));
      }
      const items = [...bank.values()];
      return Promise.resolve(
        new Response(JSON.stringify({ items, total: items.length, limit: 50, offset: 0 }), {
          status: 200, headers: { "Content-Type": "application/json" },
        }),
      );
    }),
  );
  return { deleted };
}

function renderItems() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <AuthProvider>
        <MemoryRouter>
          <Items />
        </MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("Items bulk delete", () => {
  it("select-all selects every row and deletes them after confirmation", async () => {
    const { deleted } = stubItems(["i1", "i2", "i3"]);
    vi.spyOn(window, "confirm").mockReturnValue(true);
    renderItems();

    await waitFor(() => expect(screen.getByText("i1")).toBeInTheDocument());
    await userEvent.click(screen.getByLabelText(/select all/i));

    const del = screen.getByRole("button", { name: /delete selected \(3\)/i });
    await userEvent.click(del);

    await waitFor(() => expect(deleted.sort()).toEqual(["i1", "i2", "i3"]));
    // list refetched → empty bank message
    await waitFor(() => expect(screen.getByText(/no items match/i)).toBeInTheDocument());
  });

  it("does not delete anything when the confirm is cancelled", async () => {
    const { deleted } = stubItems(["i1", "i2"]);
    vi.spyOn(window, "confirm").mockReturnValue(false);
    renderItems();

    await waitFor(() => expect(screen.getByText("i1")).toBeInTheDocument());
    const row = screen.getByText("i1").closest("tr")!;
    await userEvent.click(within(row).getByRole("checkbox"));
    await userEvent.click(screen.getByRole("button", { name: /delete selected \(1\)/i }));

    expect(deleted).toEqual([]);
    expect(screen.getByText("i1")).toBeInTheDocument();
  });
});
