import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Box, Group, Indicator, Paper, ScrollArea, Select } from "@mantine/core";
import { api, qk, type Account } from "./api";
import { useSSE } from "./useSSE";

type LogMap = Record<string, string[]>;

function opKey(accountId: number, operationId: string) {
  return `${accountId}|${operationId}`;
}

export function OutputPane({ accounts }: { accounts: Account[] }) {
  const { data: ops } = useQuery({ queryKey: qk.operations, queryFn: api.listOperations });
  const [logs, setLogs] = useState<LogMap>({});
  const [selected, setSelected] = useState<string | null>(null);
  const viewportRef = useRef<HTMLDivElement>(null);
  const atBottomRef = useRef(true);

  // Single global SSE connection: append live log lines + invalidate caches.
  useSSE((accountId, operationId, line) => {
    const key = opKey(accountId, operationId);
    setLogs((prev) => ({ ...prev, [key]: [...(prev[key] || []), line] }));
  });

  const accountMap = new Map<number, string>();
  accounts.forEach((a) => accountMap.set(a.id, a.source_user));

  // Merge persisted operations (disk) with any live-only keys present in logs.
  const seen = new Set<string>();
  const merged = [
    ...(ops || []).map((o) => ({ key: opKey(o.account_id, o.operation_id), accountId: o.account_id, opId: o.operation_id })),
    ...Object.keys(logs).map((k) => {
      const [aid, oid] = k.split("|");
      return { key: k, accountId: Number(aid), opId: oid };
    }),
  ].filter((o) => (seen.has(o.key) ? false : (seen.add(o.key), true)));

  useEffect(() => {
    if (!selected && merged.length > 0) setSelected(merged[0].key);
  }, [merged.length, selected]);

  // Load historical log for a newly selected operation not yet in live state.
  useEffect(() => {
    if (!selected || logs[selected] !== undefined) return;
    const [aidStr, oid] = selected.split("|");
    const aid = Number(aidStr);
    let cancelled = false;
    api
      .getAccountLog(aid, oid)
      .then((d) => {
        if (!cancelled && d?.content != null) {
          setLogs((prev) => ({ ...prev, [selected]: d.content.split("\n") }));
        }
      })
      .catch(() => {
        if (!cancelled) setLogs((prev) => ({ ...prev, [selected]: [] }));
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected]);

  // Jump to the bottom when the user picks a different operation.
  useEffect(() => {
    atBottomRef.current = true;
    const el = viewportRef.current;
    if (el) el.scrollTo({ top: el.scrollHeight });
  }, [selected]);

  // Follow live output only while already pinned to the bottom; if the user
  // scrolled up to read earlier output, don't yank the view back down.
  useEffect(() => {
    if (!atBottomRef.current) return;
    const el = viewportRef.current;
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
  }, [logs]);

  // Attribute each account's live last_status to its most recent log entry.
  // operation_ids are RFC3339 timestamps, so the lexicographic max per account
  // is its current/last run; only that entry gets a status indicator. This
  // naturally supports several accounts syncing at once.
  const accountById = new Map<number, Account>();
  accounts.forEach((a) => accountById.set(a.id, a));
  const newestOpByAccount = new Map<number, string>();
  for (const o of merged) {
    const cur = newestOpByAccount.get(o.accountId);
    if (cur === undefined || o.opId > cur) newestOpByAccount.set(o.accountId, o.opId);
  }
  type OpStatus = "running" | "stopped";
  const statusByKey = new Map<string, OpStatus>();
  for (const o of merged) {
    const a = accountById.get(o.accountId);
    if (!a || o.opId !== newestOpByAccount.get(o.accountId)) continue;
    if (a.last_status === "running" || a.last_status === "stopped") {
      statusByKey.set(o.key, a.last_status);
    }
  }
  const selStatus = selected ? statusByKey.get(selected) : undefined;

  const lines = selected ? logs[selected] || [] : [];
  const options = merged.map((o) => ({
    value: o.key,
    label: `${accountMap.get(o.accountId) || "#" + o.accountId} — ${o.opId}`,
  }));

  return (
    <Paper>
      <Select
        label="Logs"
        placeholder="Select a log"
        data={options}
        value={selected}
        onChange={setSelected}
        searchable
        leftSection={
          selStatus ? (
            <Indicator
              color={selStatus === "running" ? "green" : "red"}
              processing={selStatus === "running"}
              size={10}
              position="middle-start"
              offset={5}
            >
              <Box w={10} h={10} />
            </Indicator>
          ) : undefined
        }
        renderOption={(item) => {
          const st = statusByKey.get(item.option.value);
          return (
            <Group gap="xs" wrap="nowrap" align="center">
              <Indicator
                color={st === "running" ? "green" : "red"}
                processing={st === "running"}
                disabled={!st}
                size={10}
                position="middle-start"
                offset={5}
              >
                <Box w={10} h={10} />
              </Indicator>
              <span
                style={{
                  flex: 1,
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                {item.option.label}
              </span>
            </Group>
          );
        }}
      />
      <Box mt="xs" p="sm" style={{ background: "var(--mantine-color-dark-8)", borderRadius: "var(--mantine-radius-xs)" }}>
        <ScrollArea
          h={360}
          viewportRef={viewportRef}
          onScroll={(e) => {
            const el = e.currentTarget;
            atBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
          }}
        >
          <pre
            style={{
              margin: 0,
              fontFamily: "var(--mantine-font-family-monospace)",
              fontSize: 12,
              lineHeight: 1.4,
              color: "var(--mantine-color-gray-3)",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
            }}
          >
            {lines.length ? lines.join("\n") : "(no output yet — start a sync)"}
          </pre>
        </ScrollArea>
      </Box>
    </Paper>
  );
}
