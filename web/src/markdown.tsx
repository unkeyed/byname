import type { ReactNode } from "react";

/**
 * Renders the Markdown that AI answers are written in: headings, paragraphs,
 * lists, quotes, code, tables, emphasis and links. It builds React elements
 * and never HTML strings, so markup in an answer shows as text. Every run of
 * plain text goes through mark, which is where brand names get highlighted.
 */
export function Markdown({ text, mark }: { text: string; mark: (text: string) => ReactNode }) {
  return <>{blocks(text.replace(/\r\n?/g, "\n").split("\n"), mark)}</>;
}

const fence = /^\s*(```|~~~)/;
const heading = /^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
const rule = /^\s{0,3}([-*_])(\s*\1){2,}\s*$/;
const item = /^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$/;
const quote = /^\s{0,3}>\s?(.*)$/;
const tableRow = /^\s*\|.*\|\s*$/;
const tableRule = /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/;

const startsBlock = (line: string) => fence.test(line) || heading.test(line) || rule.test(line) || item.test(line) || quote.test(line) || tableRow.test(line);

function blocks(lines: string[], mark: (text: string) => ReactNode): ReactNode[] {
  const out: ReactNode[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const key = out.length;
    if (!line.trim()) {
      i++;
      continue;
    }
    if (fence.test(line)) {
      const close = line.trim().slice(0, 3);
      const body: string[] = [];
      for (i++; i < lines.length && !lines[i].trim().startsWith(close); i++) body.push(lines[i]);
      i++;
      out.push(
        <pre key={key} className="my-3 overflow-x-auto rounded-control bg-header px-3 py-2 font-mono text-xs leading-relaxed">
          <code>{body.join("\n")}</code>
        </pre>,
      );
      continue;
    }
    const h = heading.exec(line);
    if (h) {
      i++;
      out.push(
        <p key={key} role="heading" aria-level={Math.min(6, h[1].length + 3)} className={h[1].length <= 2 ? "mt-4 mb-2 text-base font-semibold" : "mt-3 mb-1.5 font-semibold"}>
          {inline(h[2], mark)}
        </p>,
      );
      continue;
    }
    if (rule.test(line)) {
      i++;
      out.push(<hr key={key} className="my-4 border-line" />);
      continue;
    }
    if (quote.test(line)) {
      const body: string[] = [];
      for (; i < lines.length && lines[i].trim(); i++) body.push(quote.exec(lines[i])?.[1] ?? lines[i]);
      out.push(
        <blockquote key={key} className="my-3 border-l-2 border-line-strong pl-3 text-muted">
          {blocks(body, mark)}
        </blockquote>,
      );
      continue;
    }
    if (tableRow.test(line) && tableRule.test(lines[i + 1] ?? "")) {
      const head = cells(line);
      const rows: string[][] = [];
      for (i += 2; i < lines.length && tableRow.test(lines[i]); i++) rows.push(cells(lines[i]));
      out.push(
        <div key={key} className="my-3 overflow-x-auto">
          <table className="w-full min-w-max border-collapse text-left text-xs">
            <thead>
              <tr className="border-b border-line-strong">
                {head.map((c, j) => (
                  <th key={j} className="py-1.5 pr-4 font-semibold">
                    {inline(c, mark)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {rows.map((r, k) => (
                <tr key={k}>
                  {head.map((_, j) => (
                    <td key={j} className="py-1.5 pr-4 align-top">
                      {inline(r[j] ?? "", mark)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>,
      );
      continue;
    }
    const first = item.exec(line);
    if (first) {
      const ordered = /\d/.test(first[2]);
      const indent = first[1].length;
      const items: string[][] = [];
      while (i < lines.length) {
        const m = item.exec(lines[i]);
        if (m && m[1].length <= indent && /\d/.test(m[2]) === ordered) {
          items.push([m[3]]);
          i++;
        } else if (items.length && lines[i].trim() && (/^\s/.test(lines[i]) || !startsBlock(lines[i]))) {
          // An indented line, or one that just runs on, belongs to the item above; a nested list stays indented so it parses as one.
          items[items.length - 1].push(m ? lines[i] : lines[i].replace(/^\s{1,4}/, ""));
          i++;
        } else if (!lines[i].trim() && /^\s+\S/.test(lines[i + 1] ?? "") && items.length) {
          i++;
        } else break;
      }
      const List = ordered ? "ol" : "ul";
      out.push(
        <List key={key} start={ordered && Number.parseInt(first[2]) !== 1 ? Number.parseInt(first[2]) : undefined} className={ordered ? "my-2 list-decimal space-y-1 pl-5" : "my-2 list-disc space-y-1 pl-5"}>
          {items.map((body, k) => (
            <li key={k} className="pl-1">
              {body.length === 1 ? inline(body[0], mark) : blocks(body, mark)}
            </li>
          ))}
        </List>,
      );
      continue;
    }
    const para: string[] = [];
    for (; i < lines.length && lines[i].trim() && (para.length === 0 || !startsBlock(lines[i])); i++) para.push(lines[i].trim());
    out.push(
      <p key={key} className="my-2 first:mt-0 last:mb-0">
        {para.map((l, k) => (
          <span key={k}>
            {k > 0 && <br />}
            {inline(l, mark)}
          </span>
        ))}
      </p>,
    );
  }
  return out;
}

function cells(row: string): string[] {
  return row
    .trim()
    .replace(/^\||\|$/g, "")
    .split(/(?<!\\)\|/)
    .map((c) => c.trim());
}

/** Code, bold, italics and links. A link keeps its text but only goes somewhere when it points at http(s) or mailto. */
const token = /`([^`]+)`|\*\*(.+?)\*\*|__(.+?)__|(?<![\w*])\*(?![\s*])(.+?)(?<!\s)\*(?!\w)|(?<!\w)_(?![\s_])(.+?)(?<!\s)_(?!\w)|\[([^\]]+)\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)/;

function inline(text: string, mark: (text: string) => ReactNode): ReactNode[] {
  const out: ReactNode[] = [];
  let rest = text;
  while (rest) {
    const m = token.exec(rest);
    if (!m) {
      out.push(<span key={out.length}>{mark(rest)}</span>);
      break;
    }
    if (m.index > 0) out.push(<span key={out.length}>{mark(rest.slice(0, m.index))}</span>);
    const key = out.length;
    const [, code, bold1, bold2, em1, em2, linkText, href] = m;
    if (code != null)
      out.push(
        <code key={key} className="rounded-inner bg-subtle px-1 py-px font-mono text-[0.85em]">
          {code}
        </code>,
      );
    else if (bold1 != null || bold2 != null) out.push(<strong key={key} className="font-semibold">{inline(bold1 ?? bold2, mark)}</strong>);
    else if (em1 != null || em2 != null) out.push(<em key={key}>{inline(em1 ?? em2, mark)}</em>);
    else if (/^(https?:|mailto:)/i.test(href))
      out.push(
        <a key={key} href={href} target="_blank" rel="noreferrer noopener" className="underline decoration-line-hover underline-offset-2 hover:decoration-fg">
          {inline(linkText, mark)}
        </a>,
      );
    else out.push(<span key={key}>{inline(linkText, mark)}</span>);
    rest = rest.slice(m.index + m[0].length);
  }
  return out;
}
