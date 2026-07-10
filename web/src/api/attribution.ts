// siteRoot reduces a URL to its origin (scheme://host) — the author's landing
// site, for a "more tests from this author" link. Returns "" when unparseable.
export function siteRoot(url: string): string {
  try {
    return new URL(url).origin;
  } catch {
    return "";
  }
}

// primaryUrl picks the best "imported from" link: the first URL that isn't an
// obvious data archive (zip/csv/tsv/json/xlsx), falling back to the first URL.
export function primaryUrl(urls: string[] | null | undefined): string {
  const list = urls ?? [];
  return list.find((u) => !/\.(zip|csv|tsv|json|xlsx?)($|\?)/i.test(u)) ?? list[0] ?? "";
}
