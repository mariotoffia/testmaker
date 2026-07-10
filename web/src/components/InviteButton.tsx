import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { api } from "../api/client";
import { useApiToken } from "../api/hooks";

// InviteButton mints an invite for a test and shows the shareable player link.
// The token is in the URL fragment (/take#…) so it never reaches server logs.
export function InviteButton({ testId }: { testId: string }) {
  const token = useApiToken();
  const [link, setLink] = useState("");
  const mint = useMutation({
    mutationFn: () => api.mintInvite(token, testId, {}),
    onSuccess: (inv) => setLink(new URL(inv.url, window.location.origin).toString()),
  });
  return (
    <div className="space-y-2">
      <button
        onClick={() => mint.mutate()}
        disabled={mint.isPending}
        className="rounded bg-slate-800 px-4 py-2 text-white disabled:opacity-50"
      >
        {mint.isPending ? "Minting…" : "Create taker invite"}
      </button>
      {mint.isError && <p className="text-sm text-red-600">Minting failed (auth mode must be “token”).</p>}
      {link && (
        <div className="flex items-center gap-2">
          <input readOnly value={link} className="w-full rounded border px-2 py-1 text-sm" aria-label="invite link" />
          <button onClick={() => navigator.clipboard?.writeText(link)} className="rounded border px-3 py-1 text-sm">Copy</button>
        </div>
      )}
    </div>
  );
}
