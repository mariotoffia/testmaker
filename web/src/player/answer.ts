import type { AnswerFormat } from "../api/types";

// Answer is the wire body for POST /api/sessions/{id}/answers, matching the Go
// answerReq (camelCase). Only the field for the item's format is meaningful.
export interface Answer {
  itemId: string;
  optionId?: string;
  numeric?: number;
  verdict?: string;
}

export function emptyAnswer(itemId: string, _format: AnswerFormat): Answer {
  void _format;
  return { itemId }; // a blank answer records as wrong (the speeded-timeout case)
}
