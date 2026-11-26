import { useId, useRef, useState, type FormEvent, type MouseEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useLocation } from "react-router";
import { api, ApiError, type Entity } from "./api";
import { keys, useIsAdmin } from "./queries";
import { go, openRoutes, useEntity } from "./routing";
import { samePage } from "./samePage";
import { Badge, Button, Card, CardHeader, cx, Dialog, DialogBody, ErrorNote, Field, Icon, Initials, inputClass, scrollArea, Spinner } from "./ui";

/** The slug the server gives a name when none is typed, as db.Slug makes it. */
export function slugify(name: string): string {
  const s = name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
    .replace(/-+$/, "");
  return s || "default";
}

function follow(e: MouseEvent<HTMLAnchorElement>, to: string) {
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  e.preventDefault();
  go(to);
}

/**
 * The open entity's name, at the top of the navigation. It opens a list of
 * every entity, with Add entity at the end for admins.
 */
export function EntitySwitcher({ rail, onNavigate }: { rail?: boolean; onNavigate?: () => void }) {
  const { slug, current, list } = useEntity();
  const admin = useIsAdmin();
  const { pathname } = useLocation();
  const id = useId();
  const menu = useRef<HTMLDivElement>(null);
  const [at, setAt] = useState({ top: 0, left: 0 });
  const [adding, setAdding] = useState(false);
  const name = current?.name ?? slug;
  const page = samePage(pathname, openRoutes());
  const wideOnly = rail ? "hidden lg:block" : "block";

  const place = (e: MouseEvent<HTMLButtonElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    setAt({ top: r.bottom + 4, left: Math.max(8, Math.min(r.left, innerWidth - 264)) });
  };

  return (
    <>
      <button
        type="button"
        popoverTarget={id}
        onClick={place}
        aria-label={`${name}, switch entity`}
        title={rail ? name : undefined}
        className={cx("flex h-11 min-w-0 items-center gap-2 rounded-control px-1.5 text-left transition-colors hover:bg-subtle md:h-8", rail ? "lg:flex-1" : "flex-1")}
      >
        <Initials label={name} size={24} className="rounded-control" />
        <span className={cx(wideOnly, "min-w-0 flex-1 truncate text-sm font-semibold")}>{name}</span>
        <Icon name="updown" className={cx(wideOnly, "size-3.5 shrink-0 text-faint")} />
      </button>
      <div
        ref={menu}
        id={id}
        popover="auto"
        style={at}
        className="m-0 w-64 rounded-overlay border border-line bg-surface p-1 text-fg shadow-floating [inset:auto]"
      >
        <p className="px-2 pt-1.5 pb-1 text-xs font-medium text-faint">Entities</p>
        <ul aria-label="Entities" className={cx("max-h-[60dvh]", scrollArea)}>
          {(list.data ?? []).map((w) => (
            <li key={w.id}>
              <a
                href={`/e/${w.slug}${page}`}
                aria-current={w.slug === slug ? "page" : undefined}
                onClick={(e) => {
                  menu.current?.hidePopover();
                  onNavigate?.();
                  if (w.slug === slug) e.preventDefault();
                  else follow(e, `/e/${w.slug}${page}`);
                }}
                className="flex h-11 items-center gap-2 rounded-control px-2 text-sm transition-colors hover:bg-subtle md:h-9"
              >
                <Initials label={w.name} size={20} className="rounded-inner" />
                <span className="min-w-0 flex-1 truncate">{w.name}</span>
                {!w.onboarded && <Badge>Not set up</Badge>}
                <Icon name="check" className={cx("size-3.5 shrink-0", w.slug !== slug && "invisible")} />
              </a>
            </li>
          ))}
        </ul>
        {admin && (
          <div className="mt-1 border-t border-line pt-1">
            <button
              type="button"
              onClick={() => {
                menu.current?.hidePopover();
                setAdding(true);
              }}
              className="flex h-11 w-full items-center gap-2 rounded-control px-2 text-sm text-muted transition-colors hover:bg-subtle hover:text-fg md:h-9"
            >
              <Icon name="plus" className="size-4" />
              Add entity
            </button>
          </div>
        )}
      </div>
      <Dialog open={adding} onClose={() => setAdding(false)} title="Add entity">
        <AddEntity
          onAdded={(w) => {
            setAdding(false);
            onNavigate?.();
            go(`/e/${w.slug}/onboarding`);
          }}
        />
      </Dialog>
    </>
  );
}

const addError = (err: unknown, slug: string) =>
  err instanceof ApiError && err.status === 409 ? new Error(`Another entity already uses the slug ${slug}. Type a different slug.`) : err;

function AddEntity({ onAdded }: { onAdded: (w: Entity) => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const derived = slugify(name.trim());
  const add = useMutation({
    mutationFn: () => api.entities.create({ name: name.trim(), slug: slug.trim() || undefined }),
    onSuccess: (w) => {
      qc.setQueryData<Entity[]>(keys.entities, (l) => l && [...l, w]);
      onAdded(w);
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (name.trim()) add.mutate();
  };
  return (
    <DialogBody>
      <form onSubmit={submit} className="space-y-4">
        <p className="text-sm text-muted">Another company or product to track, with its own brand, questions, results and settings. Your team, API keys and provider keys work in every entity.</p>
        <Field label="Name">
          <input data-autofocus required maxLength={80} value={name} onChange={(e) => setName(e.target.value)} placeholder="Acme" className={inputClass} />
        </Field>
        <Field label="Slug (optional)" hint={<>Its pages live at /e/{slug.trim() || derived}. The slug never changes.</>}>
          <input
            value={slug}
            onChange={(e) => setSlug(e.target.value)}
            placeholder={derived}
            maxLength={40}
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            className={inputClass}
          />
        </Field>
        <ErrorNote error={addError(add.error, slug.trim() || derived)} />
        <div className="flex justify-end">
          <Button type="submit" variant="primary" disabled={add.isPending || !name.trim()}>
            Add and set up
          </Button>
        </div>
      </form>
    </DialogBody>
  );
}

/** Settings > General, for admins: the open entity's name. Its slug, and so its links, stay. */
export function RenameEntity() {
  const { current } = useEntity();
  const admin = useIsAdmin();
  if (!admin || !current) return null;
  return <RenameForm key={current.id} entity={current} />;
}

function RenameForm({ entity }: { entity: Entity }) {
  const qc = useQueryClient();
  const [name, setName] = useState(entity.name);
  const rename = useMutation({
    mutationFn: () => api.entities.rename(entity.slug, name.trim()),
    onSuccess: (w) => qc.setQueryData<Entity[]>(keys.entities, (l) => l?.map((x) => (x.id === w.id ? w : x))),
  });
  const changed = name.trim() !== entity.name;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (name.trim() && changed) rename.mutate();
  };
  return (
    <Card className="mb-6">
      <CardHeader title="Entity" description={`What the entity switcher calls it. Its pages stay at /e/${entity.slug}.`} />
      <form onSubmit={submit} className="space-y-2 p-4">
        <div className="flex flex-col gap-2 sm:flex-row">
          <input aria-label="Entity name" required maxLength={80} value={name} onChange={(e) => setName(e.target.value)} className={inputClass} />
          <Button type="submit" disabled={rename.isPending || !changed || !name.trim()}>
            {rename.isSuccess && !changed ? "Saved" : "Rename"}
          </Button>
        </div>
        <ErrorNote error={rename.error} />
      </form>
    </Card>
  );
}

/**
 * Settings > General, for admins: deleting the open entity and everything
 * in it. The first one stays, since paths without /e/<slug> mean it.
 */
export function DeleteEntity() {
  const { current, list } = useEntity();
  const admin = useIsAdmin();
  const [asking, setAsking] = useState(false);
  if (!admin || !current) return null;
  if (current.default) {
    return (
      <Card className="mt-6">
        <CardHeader title="Delete entity" description="The first entity cannot be deleted: links, API keys and MCP connections without /e/<slug> in their path all lead to it." />
      </Card>
    );
  }
  const first = list.data?.find((w) => w.default);
  return (
    <Card className="mt-6">
      <CardHeader title="Delete entity" description={`Deletes ${current.name} and everything in it, for everyone. This cannot be undone.`}>
        <Button variant="danger" onClick={() => setAsking(true)}>
          <Icon name="trash" className="size-3.5" />
          Delete entity
        </Button>
      </CardHeader>
      <Dialog open={asking} onClose={() => setAsking(false)} title={`Delete ${current.name}?`}>
        <ConfirmDelete key={current.id} entity={current} onCancel={() => setAsking(false)} onDeleted={() => first && go(`/e/${first.slug}`)} />
      </Dialog>
    </Card>
  );
}

function ConfirmDelete({ entity, onCancel, onDeleted }: { entity: Entity; onCancel: () => void; onDeleted: () => void }) {
  const qc = useQueryClient();
  const [typed, setTyped] = useState("");
  const remove = useMutation({
    mutationFn: () => api.entities.remove(entity.slug),
    onSuccess: () => {
      qc.setQueryData<Entity[]>(keys.entities, (l) => l?.filter((w) => w.id !== entity.id));
      void qc.invalidateQueries({ queryKey: keys.entities });
      onDeleted();
    },
  });
  const matches = typed.trim() === entity.name;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (matches) remove.mutate();
  };
  return (
    <DialogBody>
      <form onSubmit={submit} className="space-y-4 text-sm">
        <p>
          This permanently deletes {entity.name}: its brand, competitors, questions, checks and results, search engine and site connections, social mentions and
          notification channels. Its pages at /e/{entity.slug} stop working at once, and the data is gone for good a few minutes later.
        </p>
        <p className="text-muted">Your team, API keys, connected apps and provider keys are shared by every entity and stay.</p>
        <Field label={`Type ${entity.name} to confirm`}>
          <input data-autofocus value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" spellCheck={false} className={inputClass} />
        </Field>
        <ErrorNote error={remove.error} />
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button type="submit" variant="danger" disabled={!matches || remove.isPending}>
            {remove.isPending ? <Spinner className="size-3.5 border" /> : <Icon name="trash" className="size-3.5" />}
            Delete {entity.name}
          </Button>
        </div>
      </form>
    </DialogBody>
  );
}
