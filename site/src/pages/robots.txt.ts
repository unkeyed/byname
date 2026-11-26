import type { APIRoute } from "astro";
import { textHeaders } from "../lib/docs";
import { origin } from "../lib/site";

// Every crawler, search and AI alike, may read the whole site. Google's ads
// and API crawlers skip the `*` group, so they are named in it too.
const agents = ["*", "AdsBot-Google", "AdsBot-Google-Mobile", "Mediapartners-Google", "APIs-Google"];

export const GET: APIRoute = () =>
  new Response(
    `${agents.map((a) => `User-agent: ${a}`).join("\n")}\nAllow: /\n\nSitemap: ${origin}/sitemap.xml\n`,
    { headers: textHeaders },
  );
