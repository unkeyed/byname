import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type ResultsFilter } from "../../api";
import { keys } from "../../queries";
import { Button, CodeBlock, Dialog, DialogBody, ErrorNote, Pending } from "../../ui";

/** Opens the question's content brief, a prompt for the agent that writes your site's content. Run 0 is the latest check with answers in view. */
export function AgentPromptButton({ question, run = 0, filter }: { question: number; run?: number; filter: ResultsFilter }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button size="sm" variant="secondary" onClick={() => setOpen(true)}>
        Prompt for your agent
      </Button>
      <Dialog open={open} onClose={() => setOpen(false)} title="Prompt for your agent" wide>
        <AgentPrompt question={question} run={run} filter={filter} />
      </Dialog>
    </>
  );
}

function AgentPrompt({ question, run, filter }: { question: number; run: number; filter: ResultsFilter }) {
  const brief = useQuery({ queryKey: keys.brief(question, run, filter), queryFn: () => api.brief(question, run || undefined, filter) });
  return (
    <DialogBody>
      <p className="mb-3 text-sm text-muted">Paste this into Claude Code, Cursor or another agent working in your site's repository.</p>
      {brief.isPending ? <Pending /> : brief.isError ? <ErrorNote error={brief.error} /> : <CodeBlock code={brief.data.prompt} label="Markdown" />}
    </DialogBody>
  );
}
