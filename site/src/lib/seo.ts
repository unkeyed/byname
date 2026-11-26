// Structured data for the cost page, the comparisons, and the posts.
// Nodes omit @context. The layout wraps them in one @graph.

export function articleNode(opts: { title: string; description: string; url: string; date: string }) {
  return {
    "@type": "Article",
    headline: opts.title,
    description: opts.description,
    datePublished: opts.date,
    dateModified: opts.date,
    author: { "@type": "Person", name: "James Perkins" },
    publisher: { "@type": "Organization", name: "Byname", url: "https://byname.dev" },
    mainEntityOfPage: opts.url,
  };
}

export function webPageNode(opts: { title: string; description: string; url: string; date?: string }) {
  return {
    "@type": "WebPage",
    name: opts.title,
    description: opts.description,
    url: opts.url,
    ...(opts.date ? { dateModified: opts.date } : {}),
  };
}

export function faqNode(pairs: { q: string; a: string }[]) {
  return {
    "@type": "FAQPage",
    mainEntity: pairs.map((p) => ({
      "@type": "Question",
      name: p.q,
      acceptedAnswer: { "@type": "Answer", text: p.a },
    })),
  };
}

export function graph(...nodes: object[]) {
  return { "@context": "https://schema.org", "@graph": nodes };
}
