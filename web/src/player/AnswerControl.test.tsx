import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
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

const numItem = {
  ID: "i3", AnswerFormat: "open-numeric",
  Options: [], Stimulus: [], AnswerKey: {}, Difficulty: { Band: 1 },
} as unknown as ItemSnapshot;

describe("AnswerControl (open numeric)", () => {
  it("clears to no-answer (undefined), not 0, when the field is emptied", () => {
    const onChange = vi.fn();
    render(<AnswerControl item={numItem} value={{ itemId: "i3", numeric: 5 }} onChange={onChange} onSubmit={vi.fn()} />);
    fireEvent.change(screen.getByLabelText("numeric answer"), { target: { value: "" } });
    expect(onChange).toHaveBeenLastCalledWith({ itemId: "i3", numeric: undefined });
  });

  it("records a typed value", () => {
    const onChange = vi.fn();
    render(<AnswerControl item={numItem} value={{ itemId: "i3" }} onChange={onChange} onSubmit={vi.fn()} />);
    fireEvent.change(screen.getByLabelText("numeric answer"), { target: { value: "7" } });
    expect(onChange).toHaveBeenLastCalledWith({ itemId: "i3", numeric: 7 });
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
