import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "../auth/AuthContext";
import Generate from "./Generate";

function renderGenerate() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <AuthProvider>
        <Generate />
      </AuthProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("Generate", () => {
  it("posts the generate request with the chosen body and shows success", async () => {
    const fetchMock = vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        new Response(
          JSON.stringify(url.includes("whoami") ? { role: "operator", mode: "none" } : { generated: 5 }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    renderGenerate();
    await userEvent.click(screen.getByRole("button", { name: /generate/i }));
    await waitFor(() => expect(screen.getByText(/bank has been updated/i)).toBeInTheDocument());

    const call = fetchMock.mock.calls.find((c: unknown[]) => String(c[0]).includes("/items/generate"));
    expect(call).toBeTruthy();
    const init = call![1] as RequestInit;
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toMatchObject({ testType: "A2", difficulty: 2, count: 5, seed: 1 });
  });
});
