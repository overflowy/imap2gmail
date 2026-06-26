// API client: typed fetch wrapper that attaches the double-submit CSRF token
// (read from the csrf cookie set by GET /) to all state-changing requests.
import { useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";

export type Settings = {
  client_id: string;
  client_secret: string;
  bind_port: number;
  origin_host: string;
  origin_port: number;
  origin_ssl: boolean;
  imapsync_flags: string;
  default_imapsync_flags: string;
  max_concurrent: number;
  dry_run: boolean;
  redirect_url: string;
};

export type Account = {
  id: number;
  source_user: string;
  source_password: string;
  dest_gmail: string;
  sync_checked: boolean;
  last_status: string;
  last_synced_at: string;
  authenticated: boolean;
  access_expiry: string;
  duplicate: boolean;
};

export type Operation = { account_id: number; operation_id: string; rss_bytes?: number };

function csrfToken(): string {
  const m = document.cookie.match(/(?:^|;\s*)csrf=([^;]+)/);
  return m ? m[1] : "";
}

async function apiFetch<T>(path: string, options: RequestInit = {}): Promise<T> {
  const method = options.method || "GET";
  const headers = new Headers(options.headers);
  if (options.body) headers.set("Content-Type", "application/json");
  if (method !== "GET" && method !== "HEAD") headers.set("X-CSRF-Token", csrfToken());
  const res = await fetch(path, { ...options, headers });
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    try {
      const j = await res.json();
      if (j.error) msg = j.error;
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

export const api = {
  getSettings: () => apiFetch<Settings>("/api/settings"),
  saveSettings: (s: Settings) =>
    apiFetch<Settings>("/api/settings", { method: "POST", body: JSON.stringify(s) }),
  listAccounts: () => apiFetch<Account[]>("/api/accounts"),
  addAccount: (a: { source_user: string; source_password: string; dest_gmail: string }) =>
    apiFetch<{ id: number }>("/api/accounts", { method: "POST", body: JSON.stringify(a) }),
  updateAccount: (
    id: number,
    a: { source_user: string; source_password: string; dest_gmail: string; sync_checked: boolean }
  ) => apiFetch<{ ok: boolean }>(`/api/accounts/${id}`, { method: "PUT", body: JSON.stringify(a) }),
  deleteAccount: (id: number) =>
    apiFetch<{ ok: boolean }>(`/api/accounts/${id}`, { method: "DELETE" }),
  importAccounts: (text: string) =>
    apiFetch<{ imported: number; skipped: number; errors: string[]; accounts: Account[] }>(
      "/api/accounts/import",
      { method: "POST", body: JSON.stringify({ text }) }
    ),
  setChecked: (id: number, checked: boolean) =>
    apiFetch<{ ok: boolean }>(`/api/accounts/${id}/checked`, {
      method: "POST",
      body: JSON.stringify({ checked }),
    }),
  checkAll: () => apiFetch<{ ok: boolean }>("/api/accounts/check-all", { method: "POST" }),
  checkNone: () => apiFetch<{ ok: boolean }>("/api/accounts/check-none", { method: "POST" }),
  authURL: (id: number) =>
    apiFetch<{ auth_url: string }>(`/api/accounts/${id}/auth`, { method: "POST" }),
  authExchange: (id: number, code: string) =>
    apiFetch<{ ok: boolean }>(`/api/accounts/${id}/auth/exchange`, {
      method: "POST",
      body: JSON.stringify({ code }),
    }),
  syncOne: (id: number) =>
    apiFetch<{ queued: boolean }>(`/api/accounts/${id}/sync`, { method: "POST" }),
  syncAll: () => apiFetch<{ queued: number; skipped: number }>("/api/sync", { method: "POST" }),
  stop: () => apiFetch<{ ok: boolean }>("/api/sync/stop", { method: "POST" }),
  listOperations: () => apiFetch<Operation[]>("/api/operations"),
  listAccountLogs: (id: number) =>
    apiFetch<{ operation_id: string }[]>(`/api/accounts/${id}/logs`),
  getAccountLog: (id: number, ts: string) =>
    apiFetch<{ content: string }>(`/api/accounts/${id}/log?ts=${encodeURIComponent(ts)}`),
};

export const qk = {
  settings: ["settings"] as const,
  accounts: ["accounts"] as const,
  operations: ["operations"] as const,
};

/** Convenience hook returning helpers that invalidate query keys. */
export function useInvalidate() {
  const qc = useQueryClient();
  return useCallback(
    (...keys: readonly unknown[][]) => {
      keys.forEach((k) => qc.invalidateQueries({ queryKey: k }));
    },
    [qc]
  );
}
