import { NavLink, Outlet } from "react-router-dom";
import { useAuth } from "../auth/useAuth";

const links = [
  ["/", "Dashboard"],
  ["/sources", "Sources"],
  ["/items", "Item bank"],
  ["/generate", "Generate"],
  ["/tests", "Tests"],
  ["/jobs", "Jobs"],
] as const;

export function Layout() {
  const { mode, logout } = useAuth();
  return (
    <div className="min-h-screen">
      <nav className="flex items-center gap-1 border-b bg-slate-50 px-4 py-2" aria-label="main">
        <span className="mr-4 font-semibold">Testmaker</span>
        {links.map(([to, label]) => (
          <NavLink
            key={to}
            to={to}
            end={to === "/"}
            className={({ isActive }) =>
              `rounded px-3 py-1 text-sm ${isActive ? "bg-slate-800 text-white" : "hover:bg-slate-200"}`
            }
          >
            {label}
          </NavLink>
        ))}
        {mode === "token" && (
          <button onClick={logout} className="ml-auto text-sm text-slate-500 hover:text-slate-800">
            Sign out
          </button>
        )}
      </nav>
      <main className="p-6">
        <Outlet />
      </main>
    </div>
  );
}
