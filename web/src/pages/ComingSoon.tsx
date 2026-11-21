import { EmptyState, PageHeader } from "../ui";

/** A page from the plan that has nothing to show yet. */
export function ComingSoon({ title, body }: { title: string; body: string }) {
  return (
    <>
      <PageHeader title={title} />
      <div className="relative min-h-0 flex-1 overflow-auto px-4 py-16">
        <div className="mx-auto max-w-md">
          <EmptyState icon="sparkles" title="Coming soon">
            {body}
          </EmptyState>
        </div>
      </div>
    </>
  );
}
