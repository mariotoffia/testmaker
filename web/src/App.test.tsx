import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "./auth/AuthContext";
import { AppRoutes } from "./App";

function renderAt(path: string) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation(async () =>
      new Response(JSON.stringify({ role: "operator", mode: "none" }), {
        status: 200, headers: { "Content-Type": "application/json" },
      }),
    ),
  );
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <AuthProvider>
        <MemoryRouter initialEntries={[path]}>
          <AppRoutes />
        </MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("routing", () => {
  it("renders the login page", () => {
    renderAt("/login");
    expect(screen.getByText(/operator sign-in/i)).toBeInTheDocument();
  });
  it("renders the dashboard under the guard in none mode", async () => {
    renderAt("/");
    await waitFor(() => expect(screen.getByRole("navigation")).toBeInTheDocument());
  });
});
