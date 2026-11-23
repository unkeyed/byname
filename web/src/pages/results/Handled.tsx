import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type HandledPage, type MarkedPage } from "../../api";
import { andList, shortDay, shortUrl, useLabels } from "../../lines";
import { keys, useCan, useSurfaces } from "../../queries";
import { Button, Card, CardHeader, cx, ErrorNote, focusInPlace, Icon, inputClass, Pending } from "../../ui";
import { useView } from "../../view";

/** A marked page as matching sees it: without query, fragment, scheme or www. */
const pageLabel = (u: string) => shortUrl(u.replace(/[?#].*$/, ""));

const answers = (n: number) => `${n} ${n === 1 ? "answer" : "answers"}`;

/** After a page is marked or unmarked, the missed question rows and the Handled tab both change. */
function useRefresh() {
  const qc = useQueryClient();
  return () => Promise.all([qc.invalidateQueries({ queryKey: keys.allGaps }), qc.invalidateQueries({ queryKey: keys.handled })]);
}

function useUnmark() {
  const refresh = useRefresh();
  return useMutation({ mutationFn: (id: number) => api.handled.unmark(id), onSuccess: refresh });
}

/** One line per page marked as answering a question, with a remove button for editors. */
export function MarkedPages({ pages }: { pages: MarkedPage[] }) {
  const canEdit = useCan("config:write");
  const unmark = useUnmark();
  if (pages.length === 0) return null;
  return (
    <div className="relative z-[1] space-y-0.5 text-xs">
      {pages.map((p) => (
        <p key={p.id} className="flex min-w-0 items-center gap-1.5">
          <Icon name="check" className="size-3.5 shrink-0 text-success" />
          <span className="min-w-0 [overflow-wrap:anywhere]">
            Handled by{" "}
            <a href={p.url} target="_blank" rel="noreferrer noopener" className="font-medium underline-offset-2 hover:underline">
              {pageLabel(p.url)}
            </a>
            <span className="text-muted"> · {p.linked === 0 ? "not linked yet" : `linked in ${answers(p.linked)}`}</span>
          </span>
          {canEdit && (
            <Button size="sm" variant="ghost" icon aria-label={`Remove ${pageLabel(p.url)}`} title="Remove" onClick={() => unmark.mutate(p.id)} disabled={unmark.isPending} className="-my-2 md:-my-1.5">
              <Icon name="x" className="size-3.5" />
            </Button>
          )}
        </p>
      ))}
      <ErrorNote error={unmark.error} />
    </div>
  );
}

/** For editors: mark the question as handled by a page, from a button that opens an address field. */
export function MarkHandled({ question, marked }: { question: number; marked: boolean }) {
  const canEdit = useCan("config:write");
  const refresh = useRefresh();
  const [open, setOpen] = useState(false);
  const [url, setUrl] = useState("");
  const close = () => {
    setOpen(false);
    setUrl("");
    mark.reset();
  };
  const mark = useMutation({
    mutationFn: () => api.handled.mark(question, url),
    onSuccess: () => {
      close();
      return refresh();
    },
  });
  if (!canEdit) return null;
  if (!open)
    return (
      <Button size="sm" variant="secondary" onClick={() => setOpen(true)}>
        <Icon name="check" className="size-3.5" />
        {marked ? "Add another page" : "Mark as handled"}
      </Button>
    );
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        mark.mutate();
      }}
      onKeyDown={(e) => {
        if (e.key !== "Escape") return;
        e.preventDefault();
        e.stopPropagation();
        close();
      }}
      noValidate
      className="w-full space-y-1.5"
    >
      <div className="flex flex-wrap gap-2">
        <input
          ref={focusInPlace}
          className={cx(inputClass, "basis-full sm:basis-0 sm:flex-1 md:h-7 md:text-xs")}
          inputMode="url"
          autoComplete="url"
          aria-label="The page that answers this question"
          placeholder="northwind.example/blog/your-answer"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
        />
        <Button size="sm" variant="primary" type="submit" disabled={mark.isPending || url.trim() === ""}>
          Save
        </Button>
        <Button size="sm" variant="ghost" onClick={close}>
          Cancel
        </Button>
      </div>
      <ErrorNote error={mark.error} />
    </form>
  );
}

/** The question drawer's marked pages, from the Handled list, since the drawer has no missed question row. */
export function useMarkedPages(question: number): MarkedPage[] {
  const handled = useQuery({ queryKey: keys.handled, queryFn: api.handled.list });
  return (handled.data?.pages ?? []).filter((p) => p.question.id === question).map((p) => ({ id: p.id, url: p.url, added_at: p.added_at, linked: p.linked.answers }));
}

/** Missed questions > Handled: every marked page, the answers that link to it since, and how its question names you now. */
export function HandledList() {
  const handled = useQuery({ queryKey: keys.handled, queryFn: api.handled.list });
  if (handled.isPending) return <Pending />;
  if (handled.isError) return <ErrorNote error={handled.error} />;
  const pages = handled.data.pages;
  if (pages.length === 0)
    return <Card className="px-4 py-10 text-center text-sm text-muted">No question is marked as handled yet. Once you publish a page that answers a missed question, mark it there.</Card>;
  return (
    <Card>
      <CardHeader title={`${pages.length} marked ${pages.length === 1 ? "page" : "pages"}`} description="Every AI answer since a page was marked that links to it, for any question." />
      <ul className="divide-y divide-line border-t border-line">
        {pages.map((p) => (
          <HandledRow key={p.id} page={p} />
        ))}
      </ul>
    </Card>
  );
}

function HandledRow({ page: p }: { page: HandledPage }) {
  const view = useView();
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const canEdit = useCan("config:write");
  const unmark = useUnmark();
  const apps = [...new Set(p.linked.pairs.map((pair) => labels.surfaceTitle(pair.surface)))];
  return (
    <li className="flex gap-3 px-4 py-3">
      <div className="min-w-0 flex-1 space-y-1.5">
        <button type="button" onClick={() => view.set({ q: p.question.id, qa: null, qv: null }, { push: true })} className="-my-3 py-3 text-left font-medium hover:underline md:my-0 md:py-0">
          {p.question.text}
        </button>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted">
          <a href={p.url} target="_blank" rel="noreferrer noopener" className="font-medium text-fg underline-offset-2 hover:underline [overflow-wrap:anywhere]">
            {pageLabel(p.url)}
          </a>
          <span>
            Marked {shortDay(p.added_at)}
            {p.added_by && ` by ${p.added_by}`}
          </span>
        </div>
        <p className="text-xs">
          {p.linked.answers === 0 ? (
            <span className="text-muted">Not linked yet. Byname looks for it in every answer from the next check on.</span>
          ) : (
            <>
              <span className="font-medium text-success">
                Linked in {answers(p.linked.answers)} across {p.linked.questions} {p.linked.questions === 1 ? "question" : "questions"}
              </span>
              <span className="text-muted"> by {andList(apps)}</span>
            </>
          )}
        </p>
        <Named page={p} />
        <ErrorNote error={unmark.error} />
      </div>
      {canEdit && (
        <Button size="sm" variant="ghost" icon aria-label={`Remove ${pageLabel(p.url)}`} title="Remove" onClick={() => unmark.mutate(p.id)} disabled={unmark.isPending} className="-mt-1">
          <Icon name="x" className="size-3.5" />
        </Button>
      )}
    </li>
  );
}

/** How the question's answers named you on the check before the mark and the latest since. */
function Named({ page: p }: { page: HandledPage }) {
  const of = (n: { named: number; answered: number }) => `${n.named} of ${n.answered}`;
  if (!p.before && !p.now) return null;
  const up = p.before && p.now && p.now.named / Math.max(p.now.answered, 1) > p.before.named / Math.max(p.before.answered, 1);
  return (
    <p className="text-xs text-muted tabular-nums">
      <span className="font-medium text-fg">Named you:</span> {p.before ? `before ${of(p.before)}` : "no check before"}
      {" → "}
      <span className={cx(up && "font-medium text-success")}>{p.now ? `now ${of(p.now)}` : "no check since"}</span>
    </p>
  );
}
