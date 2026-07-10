import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "../auth/AuthContext";
import Compose from "./Compose";

const composed = {
  ID: "t1", Title: "My Test", Policy: "fixed-increasing",
  Timing: { Total: 0, PerItem: 0 }, Families: ["logical"],
  Sections: [{ Title: "S1", Family: "logical", Timing: { Total: 0, PerItem: 0 }, Items: [] }],
};

function renderCompose() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <AuthProvider>
        <MemoryRouter>
          <Compose />
        </MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("Compose", () => {
  it("posts a test with a sections array and renders the composed id", async () => {
    const fetchMock = vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        new Response(
          JSON.stringify(url.includes("whoami") ? { role: "operator", mode: "none" } : composed),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    renderCompose();
    await userEvent.type(screen.getByLabelText("Test ID"), "t1");
    await userEvent.type(screen.getByLabelText("Title"), "My Test");
    await userEvent.click(screen.getByRole("button", { name: /add section/i }));
    await userEvent.click(screen.getByRole("button", { name: /compose test/i }));

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        (c: unknown[]) => String(c[0]).endsWith("/api/tests") && (c[1] as RequestInit)?.method === "POST",
      );
      expect(call).toBeTruthy();
      const body = JSON.parse((call![1] as RequestInit).body as string);
      expect(Array.isArray(body.sections)).toBe(true);
      expect(body.sections.length).toBeGreaterThanOrEqual(2);
      expect(body.id).toBe("t1");
    });
    await waitFor(() => expect(screen.getByText("t1")).toBeInTheDocument());
  });

  it("blocks an adaptive test whose section spans a single band", async () => {
    const fetchMock = vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        new Response(
          JSON.stringify(url.includes("whoami") ? { role: "operator", mode: "none" } : composed),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    renderCompose();
    await userEvent.type(screen.getByLabelText("Test ID"), "t1");
    await userEvent.selectOptions(screen.getByLabelText("Policy"), "adaptive");
    // Collapse the only section to a single band (min == max) → must be rejected client-side.
    const max = screen.getByLabelText("section 1 max difficulty");
    await userEvent.clear(max);
    await userEvent.type(max, "1");
    await userEvent.click(screen.getByRole("button", { name: /compose test/i }));

    expect(await screen.findByText(/two difficulty bands/i)).toBeInTheDocument();
    const composeCall = fetchMock.mock.calls.find(
      (c: unknown[]) => String(c[0]).endsWith("/api/tests") && (c[1] as RequestInit)?.method === "POST",
    );
    expect(composeCall).toBeUndefined(); // never posted
  });
});
