import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "../auth/AuthContext";
import { InviteButton } from "./InviteButton";

function renderButton() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <AuthProvider>
        <InviteButton testId="t1" />
      </AuthProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("InviteButton", () => {
  it("mints an invite and shows the shareable take link", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) =>
        Promise.resolve(
          new Response(
            JSON.stringify(
              url.includes("whoami")
                ? { role: "operator", mode: "token" }
                : { token: "ti.abc", url: "/take#ti.abc", expiresAt: "2026-07-09T13:00:00Z" },
            ),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        ),
      ),
    );
    renderButton();
    await userEvent.click(screen.getByRole("button", { name: /create taker invite/i }));
    const link = await screen.findByLabelText("invite link");
    await waitFor(() => expect((link as HTMLInputElement).value).toContain("/take#ti.abc"));
  });
});
