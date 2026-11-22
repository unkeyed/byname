import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api, type NamingCheck } from "../api";
import { useLoading } from "../ui";

export type QuestionNaming = NamingCheck["questions"][number];

/**
 * POST /v1/naming-check answers per text, so each answer is cached under
 * its own text: a question added from the field is already known when its
 * row appears, and the list asks only about texts it has not seen.
 */
const root = ["naming-check"] as const;
const questionKey = (text: string) => [...root, "q", text] as const;
const aliasKey = (alias: string) => [...root, "a", alias] as const;

/** The brand table decides what a question names, so a change to any brand forgets every answer. */
export const forgetNaming = (qc: QueryClient) => qc.removeQueries({ queryKey: root });

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  const key = JSON.stringify(value);
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms);
    return () => clearTimeout(id);
    // Keyed by the JSON, so a new array holding the same texts does not restart the wait.
  }, [key, ms]);
  return v;
}

const chunks = <T,>(list: T[], size: number) => Array.from({ length: Math.ceil(list.length / size) }, (_, i) => list.slice(i * size, (i + 1) * size));

/**
 * Checks texts while someone types, 300ms after they stop, by the rules
 * answers are scored with. The first check holds the step's LoadBoundary,
 * so saved questions show their marks in the first frame.
 */
function useChecked<T>(kind: "q" | "a", items: string[], read: (r: NamingCheck) => [string, T][]) {
  const qc = useQueryClient();
  const key = kind === "q" ? questionKey : aliasKey;
  const wanted = useDebounced([...new Set(items.filter(Boolean))], 300);
  const missing = wanted.filter((t) => qc.getQueryData(key(t)) === undefined);
  const batch = useQuery({
    queryKey: [...root, "batch", kind, missing],
    queryFn: async () => {
      for (const part of chunks(missing, kind === "q" ? 100 : 50)) {
        const r = await api.namingCheck(kind === "q" ? { questions: part } : { aliases: part });
        for (const [text, v] of read(r)) qc.setQueryData(key(text), v);
      }
      return missing.length;
    },
    enabled: missing.length > 0,
    staleTime: Infinity,
  });
  useLoading(batch.fetchStatus === "fetching");
  return (text: string) => qc.getQueryData<T>(key(text));
}

/** What each question names, once checked; undefined while its check is pending. */
export function useQuestionNaming(texts: string[]) {
  return useChecked<QuestionNaming>("q", texts, (r) => r.questions.map((q) => [q.text, q]));
}

/** Whether each alias is an everyday word, once checked. */
export function useCommonWords(aliases: string[]) {
  const lookup = useChecked<boolean>("a", aliases, (r) => r.aliases.map((a) => [a.alias, a.common_word]));
  return aliases.filter((a) => lookup(a) === true);
}

/** For a field being typed in: the last answer stays up while the next one is checked, so the note does not blink with every key. */
export function useSteady<T>(value: T | undefined, cleared: boolean): T | undefined {
  const last = useRef<T | undefined>(value);
  if (cleared) last.current = undefined;
  else if (value !== undefined) last.current = value;
  return last.current;
}
