import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../auth/useAuth";

// Login stores the operator token (from the server's config on first run) and
// verifies it via whoami. There is no password/account — single-tenant by
// design (ADR-0006); this is the operator credential, nothing more.
export default function Login() {
  const { login } = useAuth();
  const nav = useNavigate();
  const [token, setToken] = useState("");
  const [err, setErr] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    setErr("");
    try {
      await login(token.trim());
      nav("/");
    } catch {
      setErr("That operator token was not accepted.");
    }
  }
  return (
    <form onSubmit={submit} className="mx-auto mt-24 max-w-sm space-y-4 p-6">
      <h1 className="text-xl font-semibold">Operator sign-in</h1>
      <input
        type="password"
        value={token}
        onChange={(e) => setToken(e.target.value)}
        placeholder="operator token"
        className="w-full rounded border px-3 py-2"
        aria-label="operator token"
      />
      {err && <p className="text-sm text-red-600">{err}</p>}
      <button className="w-full rounded bg-slate-800 py-2 text-white">Sign in</button>
    </form>
  );
}
