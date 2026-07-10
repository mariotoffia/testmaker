import type { Option, StimulusPart } from "../api/types";

// mediaSrc resolves a media ref to a URL: an inline data: URI is used directly
// (the generator emits self-contained SVG), any other non-empty ref resolves
// through the content-addressed media endpoint (DESIGN §7.1 / ADR-0003).
// eslint-disable-next-line react-refresh/only-export-components
export function mediaSrc(ref: string): string {
  return ref.startsWith("data:") ? ref : `/api/media/${ref}`;
}

export function MediaRenderer({ part }: { part: StimulusPart | Option }) {
  const text = "Text" in part ? part.Text : "";
  return (
    <span className="inline-flex items-center gap-2">
      {text && <span>{text}</span>}
      {part.MediaRef && (
        <img src={mediaSrc(part.MediaRef)} alt={text || "figure"} className="max-h-40 max-w-full" />
      )}
    </span>
  );
}
