import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AnswerControl } from "./AnswerControl";
import type { ItemSnapshot } from "../api/types";

const mcItem = {
  ID: "i1", AnswerFormat: "multiple-choice",
  Options: [{ ID: "a", Text: "Alpha" }, { ID: "b", Text: "Beta" }, { ID: "c", Text: "Gamma" }],
  Stimulus: [], AnswerKey: {}, Difficulty: { Band: 1 },
} as unknown as ItemSnapshot;

const tfcItem = {
  ID: "i2", AnswerFormat: "true-false-cannotsay",
  Options: [], Stimulus: [], AnswerKey: {}, Difficulty: { Band: 1 },
} as unknown as ItemSnapshot;

describe("AnswerControl (multiple choice)", () => {
  it("selects an option by digit key and submits on Enter", async () => {
    const onChange = vi.fn();
    const onSubmit = vi.fn();
    render(<AnswerControl item={mcItem} value={{ itemId: "i1" }} onChange={onChange} onSubmit={onSubmit} />);
    await userEvent.keyboard("2"); // selects option b (2nd)
    expect(onChange).toHaveBeenLastCalledWith({ itemId: "i1", optionId: "b" });
    await userEvent.keyboard("{Enter}");
    expect(onSubmit).toHaveBeenCalled();
  });
});

describe("AnswerControl (true/false/cannot-say)", () => {
  it("picks a verdict by letter key", async () => {
    const onChange = vi.fn();
    render(<AnswerControl item={tfcItem} value={{ itemId: "i2" }} onChange={onChange} onSubmit={vi.fn()} />);
    await userEvent.keyboard("c");
    expect(onChange).toHaveBeenLastCalledWith({ itemId: "i2", verdict: "cannot-say" });
  });
});
