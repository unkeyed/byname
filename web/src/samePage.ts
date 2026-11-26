/** The part of a route object samePage reads; react-router's routes fit it. */
export type Route = { path?: string; index?: boolean; children?: Route[] };

/**
 * The page at pathname that another entity has too, for the switcher to
 * open there: the longest start of pathname that is a page in routes, or ""
 * for the entity's home. What follows it, such as a record's id, names
 * this entity's data and is dropped.
 */
export function samePage(pathname: string, routes: Route[]): string {
  const pages = new Set<string>();
  const walk = (rs: Route[], base: string) => {
    for (const r of rs) {
      // A page's path stops at its first parameter or splat.
      const fixed = (r.path ?? "").split("/").filter(Boolean);
      const stop = fixed.findIndex((s) => s.startsWith(":") || s === "*");
      const path = [base, ...(stop < 0 ? fixed : fixed.slice(0, stop))].filter(Boolean).join("/");
      if (stop !== 0) pages.add("/" + path);
      if (r.children) walk(r.children, path);
    }
  };
  walk(routes, "");
  const parts = pathname.split("/").filter(Boolean);
  for (let n = parts.length; n > 0; n--) {
    const p = "/" + parts.slice(0, n).join("/");
    if (pages.has(p)) return p;
  }
  return "";
}
