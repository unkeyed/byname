import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type QuestionRef } from "../../api";
import { keys, useCan } from "../../queries";
import { Badge, Button, cx, ErrorNote } from "../../ui";

/** Why a question can be left out, in one sentence a person reads beside it. */
export const leftOutWhy = "It names your brand, so AI names you because you asked. Byname still asks it and shows the answers, but leaves them out of your scores.";

/** Marks a question whose answers count in no score. */
export function LeftOutBadge({ question }: { question: QuestionRef }) {
  if (question.counts_in_scores) return null;
  return <Badge tone="warn">Left out of scores</Badge>;
}

/**
 * Counts a left-out question's answers in scores. It sits above a row's
 * RowButton, so it stays its own target.
 */
export function CountAnyway({ question, className }: { question: QuestionRef; className?: string }) {
  const qc = useQueryClient();
  const canEdit = useCan("config:write");
  const save = useMutation({
    mutationFn: () => api.questions.setScoring(question.id, true),
    onSuccess: () => Promise.all([qc.invalidateQueries({ queryKey: keys.results }), qc.invalidateQueries({ queryKey: keys.questionSet })]),
  });
  if (question.counts_in_scores || !canEdit) return null;
  return (
    <span className={cx("relative z-[3] flex flex-col items-start gap-1", className)}>
      <Button size="sm" variant="ghost" onClick={() => save.mutate()} disabled={save.isPending} className="-ml-2">
        Count it anyway
      </Button>
      <ErrorNote error={save.error} />
    </span>
  );
}
