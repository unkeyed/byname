import { Link } from "react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../../api";
import { keys, useMe } from "../../queries";
import { Badge, Card, CodeBlock, ConfirmButton, EmptyState, ErrorNote, Icon, PageSpinner, RelativeTime } from "../../ui";
import { SettingsHeader, SettingsSubhead } from "./Settings";

export function snippets(origin: string) {
  return {
    claude: `claude mcp add --transport http byname ${origin}/mcp`,
    cursor: JSON.stringify({ mcpServers: { byname: { url: `${origin}/mcp` } } }, null, 2),
    curl: `curl ${origin}/v1/status \\\n  -H "Authorization: Bearer <your-api-key>"`,
  };
}

export function Apps() {
  const me = useMe();
  const s = snippets(location.origin);

  return (
    <>
      <SettingsHeader title="Apps">
        Give an agent what your team sees. The MCP server at <code className="font-mono text-fg">/mcp</code> lets it read results as the person who
        connects it; the REST API does the same over HTTP with an API key.
      </SettingsHeader>

      <section className="space-y-4">
        <SettingsSubhead className="mb-0">Connect an MCP client</SettingsSubhead>
        <CodeBlock label="Claude Code" code={s.claude} />
        <CodeBlock label="Cursor (.cursor/mcp.json)" code={s.cursor} />
        <p className="text-sm text-muted">
          There is no key to paste. The first time the agent reaches Byname, it opens your browser: sign in, check the app&apos;s name and where it sends
          you back, choose what it may do, and approve. From then on it acts as{" "}
          <span className="font-medium text-fg">Claude Code (via {me.data?.name ?? "you"})</span>, never with more than your role allows.
        </p>
      </section>

      <section className="mt-8">
        <SettingsSubhead>Connected apps</SettingsSubhead>
        <ConnectedApps />
      </section>

      <section className="mt-8 space-y-4">
        <SettingsSubhead className="mb-0">REST API</SettingsSubhead>
        <CodeBlock label="curl" code={s.curl} />
        <p className="text-xs text-muted">
          Create a key under{" "}
          <Link to="/settings/keys" className="text-fg underline underline-offset-2 hover:opacity-70">
            API keys
          </Link>
          . Every route is described in{" "}
          <a href="/v1/openapi.yaml" className="text-fg underline underline-offset-2 hover:opacity-70">
            openapi.yaml
          </a>
          .
        </p>
      </section>
    </>
  );
}

/** Every app someone on the team approved, so an admin can cut one off. */
function ConnectedApps() {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: keys.apps, queryFn: async () => (await api.apps.list()) ?? [] });
  const revoke = useMutation({
    mutationFn: api.apps.revoke,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.apps }),
  });

  if (list.isPending) return <PageSpinner />;
  return (
    <>
      {list.data?.length === 0 ? (
        <EmptyState icon="plug" title="No apps connected">
          An MCP client shows up here once someone approves it in the browser.
        </EmptyState>
      ) : (
        <Card>
          <ul aria-label="Connected apps" className="divide-y divide-line">
            {list.data?.map((a) => (
              <li key={a.id} className="flex items-center gap-3 px-4 py-3">
                <span className="grid size-8 shrink-0 place-items-center rounded-control bg-subtle text-muted">
                  <Icon name="bot" className="size-4" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
                    <span className="min-w-0 truncate font-medium" title={a.name}>
                      {a.name}
                    </span>
                    <span className="min-w-0 truncate text-xs text-muted">acts as {a.approved_by}</span>
                    {a.scopes.map((s) => (
                      <Badge key={s} tone="info">
                        {s}
                      </Badge>
                    ))}
                  </div>
                  <div className="text-xs text-muted">
                    Approved <RelativeTime iso={a.created_at} /> · {a.last_used_at ? <>used <RelativeTime iso={a.last_used_at} /></> : "never used"}
                  </div>
                </div>
                <ConfirmButton label={`Disconnect ${a.name}`} confirmLabel="Disconnect?" onConfirm={() => revoke.mutate(a.id)} disabled={revoke.isPending}>
                  Disconnect
                </ConfirmButton>
              </li>
            ))}
          </ul>
        </Card>
      )}
      <div className="mt-2 empty:hidden">
        <ErrorNote error={list.error ?? revoke.error} />
      </div>
    </>
  );
}
