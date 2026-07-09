// Wire types mirror the Go delivery API (DESIGN §7.1). Domain snapshots are
// PascalCase and marshalled as-is; cmd-local bodies (jobs, invites, pages) are
// camelCase. Nullable Go slices arrive as null → model as `T[] | null`.
// time.Duration is nanoseconds; time.Time is RFC3339.

export type Ns = number;
export const NS_PER_MS = 1_000_000;
export const NS_PER_SEC = 1_000_000_000;

export type AnswerFormat = "multiple-choice" | "open-numeric" | "true-false-cannotsay";
export type SessionState = "created" | "in-progress" | "completed" | "abandoned";
export type DeliveryPolicy = "fixed-increasing" | "adaptive";
export type MediaKind = "" | "image" | "svg" | "grid" | "figure";

export interface Page<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

export interface StimulusPart {
  Text: string;
  MediaKind: MediaKind;
  MediaRef: string;
}
export interface Option {
  ID: string;
  Text: string;
  MediaKind: MediaKind;
  MediaRef: string;
}
export interface AnswerKey {
  OptionID: string;
  Numeric: number;
  Verdict: string;
  Tolerance: number;
}
export interface Difficulty {
  Band: number;
}
export interface ItemSnapshot {
  ID: string;
  Provenance: { SourceID: string; Origin: string; Redistributable: string };
  TestType: string;
  Family: string;
  Stimulus: StimulusPart[] | null;
  AnswerFormat: AnswerFormat;
  Options: Option[] | null;
  AnswerKey: AnswerKey; // present in the operator bank view; absent-valued (zero) on the taker's presented item
  Explanation: string;
  Difficulty: Difficulty;
}

export interface Timing {
  Total: Ns;
  PerItem: Ns;
}
export interface Presented {
  ItemID: string;
  Difficulty: number;
  Section: number;
  DeliveredAt: string;
}
export interface SessionSnapshot {
  ID: string;
  TestID: string;
  Policy: DeliveryPolicy;
  State: SessionState;
  Timing: Timing;
  StartedAt: string;
  EndedAt: string;
  Presented: Presented;
  Responses: unknown[] | null;
  Version: number;
}
export interface Delivery {
  Session: SessionSnapshot;
  Item: ItemSnapshot | null;
  Deadline: string;
}
export interface StartResponse extends Delivery {
  SessionToken: string;
}

export interface SectionSnapshot {
  Title: string;
  Family: string;
  Timing: Timing;
  Items: { ItemID: string; Difficulty: number }[] | null;
}
export interface TestSnapshot {
  ID: string;
  Title: string;
  Policy: DeliveryPolicy;
  Timing: Timing;
  Families: string[] | null;
  Sections: SectionSnapshot[] | null;
}

export interface ItemFeedback {
  ItemID: string;
  Correct: boolean;
  Given: string;
  CorrectAnswer: string;
  Explanation: string;
  Elapsed: Ns;
}
export interface Score {
  Raw: number;
  Max: number;
  Ability: number;
  Normed: boolean;
  Percentile: number;
  ScaledIQ: number;
  Band: string;
  Speed: { Total: Ns; Mean: Ns; CorrectPerMinute: number };
  Items: ItemFeedback[] | null;
  DegradedFeedback: number;
}

export interface SourceSnapshot {
  ID: string;
  Name: string;
  Provider: string;
  Category: string;
  Families: string[] | null;
  TestTypes: string[] | null;
  License: { Category: string; Detail: string; Redistributable: string };
  Extraction: { Method: string; Auth: string; ItemsAs: string; Notes: string };
  ItemCount: number;
}

// cmd-local (camelCase)
export interface Job {
  id: string;
  kind: "ingest" | "ingest-llm";
  sourceId: string;
  state: "queued" | "running" | "done" | "failed";
  report?: IngestReport;
  error?: string;
  createdAt: string;
  startedAt: string;
  endedAt: string;
}
export interface IngestReport {
  SourceID: string;
  Fetched: number;
  Normalized: number;
  Saved: number;
  Skipped: number;
  Note: string;
}
export interface Invite {
  token: string;
  url: string;
  expiresAt: string;
}
export interface InvitePreview {
  testId: string;
  title: string;
  policy: DeliveryPolicy;
  totalSeconds: number;
  perItemSeconds: number;
  itemCount: number;
  sections: { title: string; family: string; itemCount: number; totalSeconds: number; perItemSeconds: number }[];
  expiresAt: string;
}
export interface Whoami {
  role: "operator" | "taker" | "anonymous";
  mode: "token" | "none";
}
