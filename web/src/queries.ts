import { useQuery } from "@tanstack/react-query";
import { api } from "./api";

/** Query keys in one place, so a mutation can invalidate exactly what it touched. */
export const keys = {
  me: ["me"] as const,
  keys: ["keys"] as const,
  apps: ["apps"] as const,
  consent: (request: string) => ["consent", request] as const,
  users: ["users"] as const,
  account: ["account"] as const,
  authOptions: ["auth-options"] as const,
  secondFactor: ["second-factor"] as const,
  invite: (token: string) => ["invite", token] as const,
  setup: (token: string) => ["setup", token] as const,
};

export const useMe = () => useQuery({ queryKey: keys.me, queryFn: api.me.get, staleTime: Infinity });

export const useAuthOptions = () => useQuery({ queryKey: keys.authOptions, queryFn: api.auth.options, staleTime: 60_000 });

export const useAccount = () => useQuery({ queryKey: keys.account, queryFn: api.account.get });
