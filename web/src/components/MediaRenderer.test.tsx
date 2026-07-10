import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MediaRenderer } from "./MediaRenderer";
import type { StimulusPart } from "../api/types";

describe("MediaRenderer", () => {
  it("renders an inline data: URI directly", () => {
    const part: StimulusPart = { Text: "", MediaKind: "svg", MediaRef: "data:image/svg+xml;base64,AAA" };
    render(<MediaRenderer part={part} />);
    expect(screen.getByRole("img")).toHaveAttribute("src", "data:image/svg+xml;base64,AAA");
  });

  it("resolves a blob ref through the media endpoint", () => {
    const part: StimulusPart = { Text: "", MediaKind: "image", MediaRef: "abc123" };
    render(<MediaRenderer part={part} />);
    expect(screen.getByRole("img")).toHaveAttribute("src", "/api/media/abc123");
  });

  it("renders a text-only part with no image", () => {
    const part: StimulusPart = { Text: "hello", MediaKind: "", MediaRef: "" };
    render(<MediaRenderer part={part} />);
    expect(screen.getByText("hello")).toBeInTheDocument();
    expect(screen.queryByRole("img")).toBeNull();
  });
});
