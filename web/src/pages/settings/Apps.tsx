import { Link } from "react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../../api";
import { keys } from "../../queries";
import { useWorkspace } from "../../routing";
import { Badge, Card, CodeBlock, ConfirmButton, EmptyState, ErrorNote, Icon, Pending, RelativeTime } from "../../ui";
import { SettingsHeader, SettingsSubhead } from "./Settings";

function snippets(base: string) {
  return {
    claude: `claude mcp add --transport http byname ${base}/mcp`,
    cursor: JSON.stringify({ mcpServers: { byname: { url: `${base}/mcp` } } }, null, 2),
    curl: `curl ${base}/v1/status \\\n  -H "Authorization: Bearer <your-api-key>"`,
  };
}

export function Apps() {
  const { slug, current } = useWorkspace();
  const s = snippets(current?.default ? location.origin : `${location.origin}/w/${slug}`);

  return (
    <>
      <SettingsHeader title="Apps" />

      <section className="space-y-4">
        <SettingsSubhead className="mb-0">Connect an MCP client</SettingsSubhead>
        <CodeBlock label="Claude Code" code={s.claude} />
        <CodeBlock label="Cursor (.cursor/mcp.json)" code={s.cursor} />
        {current && <p className="text-xs text-muted">This address reads and changes {current.name} only. Each entity has its own, and one sign-in covers all of them.</p>}
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

  if (list.isPending) return <Pending />;
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
