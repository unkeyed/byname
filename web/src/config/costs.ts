import { useQuery, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { usd } from "../lines";
import { useLoading } from "../ui";

const costsKey = ["costs"] as const;

/** GET /v1/costs. Every setup change that moves a cost refreshes it with refreshCosts. It holds the nearest LoadBoundary on its first load. */
export function useCosts() {
  const costs = useQuery({ queryKey: costsKey, queryFn: api.costs });
  useLoading(costs.isPending);
  return costs;
}

export const refreshCosts = (qc: QueryClient) => qc.invalidateQueries({ queryKey: costsKey });

/** The average calendar month, as internal/config counts it. */
const daysPerMonth = 365.25 / 12;

/** What a check costs in an average month when it runs every few days. */
export const monthOfChecks = (perCheck: number, everyDays: number) => (everyDays > 0 ? (perCheck * daysPerMonth) / everyDays : 0);

/** "about $0.36", rounded to what a reader can use: whole cents from a dollar up, two significant figures below. */
export function about(v: number): string {
  if (v >= 1) return `about ${usd(Math.round(v * 100) / 100)}`;
  if (v === 0) return "$0";
  return `about ${usd(Number(v.toPrecision(2)))}`;
}

/** about() to open a sentence. */
export const About = (v: number) => {
  const s = about(v);
  return s[0].toUpperCase() + s.slice(1);
};
