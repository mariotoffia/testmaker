import { Route, Routes } from "react-router-dom";
import { AuthProvider } from "./auth/AuthContext";
import { RequireOperator } from "./auth/RequireOperator";
import { Layout } from "./components/Layout";
import Login from "./pages/Login";
import NotFound from "./pages/NotFound";
import Dashboard from "./pages/Dashboard";
import Sources from "./pages/Sources";
import Items from "./pages/Items";
import Generate from "./pages/Generate";
import Compose from "./pages/Compose";
import Tests from "./pages/Tests";
import Jobs from "./pages/Jobs";
import Take from "./pages/Take";

// AppRoutes is separated from App so tests can mount it inside a MemoryRouter.
export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/take" element={<Take />} />
      <Route element={<RequireOperator />}>
        <Route element={<Layout />}>
          <Route index element={<Dashboard />} />
          <Route path="sources" element={<Sources />} />
          <Route path="items" element={<Items />} />
          <Route path="generate" element={<Generate />} />
          <Route path="compose" element={<Compose />} />
          <Route path="tests" element={<Tests />} />
          <Route path="jobs" element={<Jobs />} />
        </Route>
      </Route>
      <Route path="*" element={<NotFound />} />
    </Routes>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <AppRoutes />
    </AuthProvider>
  );
}
