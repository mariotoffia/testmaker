import { Navigate, Outlet } from "react-router-dom";
import { useAuth } from "./useAuth";

// RequireOperator guards the console. In none mode the server reports everyone
// as operator, so this is transparently open for local development.
export function RequireOperator() {
  const { role } = useAuth();
  if (role !== "operator") return <Navigate to="/login" replace />;
  return <Outlet />;
}
