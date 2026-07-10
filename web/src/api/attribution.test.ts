import { describe, expect, it } from "vitest";
import { siteRoot, primaryUrl } from "./attribution";

describe("attribution helpers", () => {
  it("siteRoot reduces a URL to its origin, or '' when unparseable", () => {
    expect(siteRoot("https://www.123test.com/iq-test/")).toBe("https://www.123test.com");
    expect(siteRoot("https://openpsychometrics.org/_rawdata/x.zip")).toBe("https://openpsychometrics.org");
    expect(siteRoot("not a url")).toBe("");
    expect(siteRoot("")).toBe("");
  });

  it("primaryUrl prefers the first non-archive URL, else the first", () => {
    expect(primaryUrl(["https://x.io/data.zip", "https://x.io/take/"])).toBe("https://x.io/take/");
    expect(primaryUrl(["https://x.io/only.zip"])).toBe("https://x.io/only.zip");
    expect(primaryUrl(["https://x.io/page/"])).toBe("https://x.io/page/");
    expect(primaryUrl(null)).toBe("");
    expect(primaryUrl([])).toBe("");
  });
});
