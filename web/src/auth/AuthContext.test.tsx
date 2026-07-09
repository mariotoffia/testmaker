import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider } from "./AuthContext";
import { useAuth } from "./useAuth";

function Probe() {
  const { role, login } = useAuth();
  return (
    <div>
      <span>role:{role}</span>
      <button onClick={() => login("OP")}>login</button>
    </div>
  );
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("AuthProvider", () => {
  it("resolves role via whoami after login", async () => {
    // AuthProvider calls whoami twice (mount + login); a Response body is a
    // one-shot stream, so each fetch must get a FRESH Response, not one shared
    // instance (which the second read would find already-consumed).
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation(async () =>
        new Response(JSON.stringify({ role: "operator", mode: "token" }), {
          status: 200, headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    );
    await userEvent.click(screen.getByText("login"));
    await waitFor(() => expect(screen.getByText("role:operator")).toBeInTheDocument());
    expect(localStorage.getItem("tm.operatorToken")).toBe("OP");
  });
});
