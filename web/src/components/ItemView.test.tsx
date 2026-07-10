import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { ItemView } from "./ItemView";
import type { ItemSnapshot } from "../api/types";

const item: ItemSnapshot = {
  ID: "i1",
  Provenance: { SourceID: "s", Origin: "generated", Redistributable: "yes" },
  TestType: "A2",
  Family: "logical",
  Stimulus: [{ Text: "What comes next?", MediaKind: "", MediaRef: "" }],
  AnswerFormat: "multiple-choice",
  Options: [
    { ID: "a", Text: "Alpha", MediaKind: "", MediaRef: "" },
    { ID: "b", Text: "Beta", MediaKind: "", MediaRef: "" },
  ],
  AnswerKey: { OptionID: "b", Numeric: 0, Verdict: "", Tolerance: 0 },
  Explanation: "Because beta.",
  Difficulty: { Band: 2 },
};

describe("ItemView", () => {
  it("highlights the correct option and shows the explanation when showKey", () => {
    render(<ItemView item={item} showKey />);
    expect(screen.getByText("What comes next?")).toBeInTheDocument();
    expect(screen.getByText("Because beta.")).toBeInTheDocument();
    const beta = screen.getByText("Beta").closest("li");
    expect(beta?.className).toContain("border-green-500");
    const alpha = screen.getByText("Alpha").closest("li");
    expect(alpha?.className).not.toContain("border-green-500");
  });

  it("hides the key and explanation when showKey is false", () => {
    render(<ItemView item={item} showKey={false} />);
    expect(screen.queryByText("Because beta.")).toBeNull();
    const beta = screen.getByText("Beta").closest("li");
    expect(beta?.className).not.toContain("border-green-500");
  });
});
