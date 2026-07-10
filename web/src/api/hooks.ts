import { useQuery } from "@tanstack/react-query";
import { api } from "./client";
import { useAuth } from "../auth/useAuth";

// useApiToken returns the operator token (empty in none mode — the server
// ignores it there). Every operator query threads it so a token-mode deployment
// authorizes and a none-mode one is transparently open.
export function useApiToken(): string {
  return useAuth().operatorToken;
}

export function useSources(q = "") {
  const token = useApiToken();
  return useQuery({ queryKey: ["sources", q], queryFn: () => api.listSources(token, q) });
}
export function useSource(id: string) {
  const token = useApiToken();
  return useQuery({ queryKey: ["source", id], queryFn: () => api.getSource(token, id), enabled: !!id });
}
export function useItems(q = "") {
  const token = useApiToken();
  return useQuery({ queryKey: ["items", q], queryFn: () => api.listItems(token, q) });
}
export function useItem(id: string) {
  const token = useApiToken();
  return useQuery({ queryKey: ["item", id], queryFn: () => api.getItem(token, id), enabled: !!id });
}
export function useTests(q = "") {
  const token = useApiToken();
  return useQuery({ queryKey: ["tests", q], queryFn: () => api.listTests(token, q) });
}
export function useJobs(pollMs = 0) {
  const token = useApiToken();
  return useQuery({
    queryKey: ["jobs"],
    queryFn: () => api.listJobs(token),
    refetchInterval: pollMs || false,
  });
}
export function useJob(id: string, pollMs = 0) {
  const token = useApiToken();
  return useQuery({
    queryKey: ["job", id],
    queryFn: () => api.getJob(token, id),
    enabled: !!id,
    refetchInterval: (query) => {
      const s = query.state.data?.state;
      return pollMs && s !== "done" && s !== "failed" ? pollMs : false;
    },
  });
}
