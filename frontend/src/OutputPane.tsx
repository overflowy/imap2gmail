import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Anchor, Box, Group, Indicator, Paper, ScrollArea, Select, Text } from "@mantine/core";
import { api, qk, type Account } from "./api";
import { useSSE } from "./useSSE";

type LogMap = Record<string, string[]>;
type RssMap = Record<string, number>;
const LOG_HEIGHT = 550;

function opKey(accountId: number, operationId: string) {
  return `${accountId}|${operationId}`;
}

function stripRSS(line: string) {
  return line.replace(/\s+\[RSS [^\]]+\]$/, "");
}

function formatRSS(bytes?: number) {
  if (!bytes || bytes <= 0) return "";
  const mb = bytes / 1024 / 1024;
  if (mb >= 1024) {
    const gb = mb / 1024;
    return `[${gb >= 10 ? Math.round(gb) : gb.toFixed(1)}GB]`;
  }
  return `[${Math.max(1, Math.round(mb))}MB]`;
}

function isNearBottom(el: HTMLDivElement) {
  return el.scrollHeight - el.scrollTop - el.clientHeight < 40;
}

export function OutputPane({
  accounts,
  syncSelect,
}: {
  accounts: Account[];
  syncSelect: { accountId: number; token: number } | null;
}) {
  const { data: ops } = useQuery({ queryKey: qk.operations, queryFn: api.listOperations });
  const [logs, setLogs] = useState<LogMap>({});
  const [rssByKey, setRssByKey] = useState<RssMap>({});
  const [selected, setSelected] = useState<string | null>(null);
  const viewportRef = useRef<HTMLDivElement>(null);
  const atBottomRef = useRef(true);
  const programmaticFollowRef = useRef(false);
  // When a sync is started, auto-select the target account's next operation log
  // once its "operation" event arrives.
  const autoSelectAccountId = useRef<number | null>(null);

  useEffect(() => {
    if (syncSelect) autoSelectAccountId.current = syncSelect.accountId;
  }, [syncSelect]);

  // Single global SSE connection: append live log lines + invalidate caches.
  useSSE(
    (accountId, operationId, line, rssBytes) => {
      const key = opKey(accountId, operationId);
      setLogs((prev) => ({ ...prev, [key]: [...(prev[key] || []), stripRSS(line)] }));
      if (rssBytes && rssBytes > 0) {
        setRssByKey((prev) => ({ ...prev, [key]: Math.max(prev[key] || 0, rssBytes) }));
      }
    },
    (accountId, operationId) => {
      if (autoSelectAccountId.current === accountId) {
        autoSelectAccountId.current = null;
        setSelected(opKey(accountId, operationId));
      }
    },
  );

  const resetScroll = (behavior: ScrollBehavior = "auto") => {
    atBottomRef.current = true;
    programmaticFollowRef.current = behavior === "smooth";
    const el = viewportRef.current;
    if (!el) return;
    el.scrollTo({ top: el.scrollHeight, behavior });
  };

  const pauseAutoFollow = () => {
    atBottomRef.current = false;
    programmaticFollowRef.current = false;
  };

  useEffect(() => {
    const el = viewportRef.current;
    if (!el) return;
    const onScroll = () => {
      const atBottom = isNearBottom(el);
      if (programmaticFollowRef.current && !atBottom) {
        atBottomRef.current = true;
        return;
      }
      if (atBottom) programmaticFollowRef.current = false;
      atBottomRef.current = atBottom;
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    onScroll();
    return () => el.removeEventListener("scroll", onScroll);
  }, []);

  const accountMap = new Map<number, string>();
  accounts.forEach((a) => accountMap.set(a.id, a.source_user));

  // Merge persisted operations (disk) with any live-only keys present in logs.
  const seen = new Set<string>();
  const merged = [
    ...(ops || []).map((o) => ({ key: opKey(o.account_id, o.operation_id), accountId: o.account_id, opId: o.operation_id, rssBytes: o.rss_bytes })),
    ...Object.keys(logs).map((k) => {
      const [aid, oid] = k.split("|");
      return { key: k, accountId: Number(aid), opId: oid, rssBytes: undefined };
    }),
  ]
    .filter((o) => (seen.has(o.key) ? false : (seen.add(o.key), true)))
    .sort((a, b) => b.opId.localeCompare(a.opId));

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
          setLogs((prev) => ({ ...prev, [selected]: d.content.split("\n").map(stripRSS) }));
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
    resetScroll();
  }, [selected]);

  // Follow live output only while explicitly pinned to the bottom. User scrolls
  // away from the bottom pause following until the title link is clicked.
  useEffect(() => {
    if (!atBottomRef.current) return;
    const el = viewportRef.current;
    if (el) requestAnimationFrame(() => el.scrollTo({ top: el.scrollHeight, behavior: "smooth" }));
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
  const persistedRssByKey = new Map<string, number>();
  for (const o of merged) {
    if (o.rssBytes) persistedRssByKey.set(o.key, o.rssBytes);
  }
  const rssForKey = (key: string) => rssByKey[key] || persistedRssByKey.get(key) || 0;
  const selectedRSS = selected ? formatRSS(rssForKey(selected)) : "";

  const lines = selected ? logs[selected] || [] : [];
  const options = merged.map((o) => ({
    value: o.key,
    label: `${accountMap.get(o.accountId) || "#" + o.accountId} — ${o.opId}`,
  }));

  return (
    <Paper>
      <Group justify="space-between" align="center" gap="xs" wrap="nowrap" mb={4}>
        <Text size="sm" fw={500}>
          Logs
        </Text>
        <Anchor
          component="button"
          type="button"
          size="xs"
          c="dimmed"
          onClick={() => resetScroll("smooth")}
        >
          jump to latest
        </Anchor>
      </Group>
      <Select
        placeholder="Select a log"
        data={options}
        value={selected}
        onChange={setSelected}
        searchable
        rightSection={
          selectedRSS ? (
            <Text size="xs" c="dimmed" ff="var(--mantine-font-family-monospace)">
              {selectedRSS}
            </Text>
          ) : undefined
        }
        rightSectionWidth={selectedRSS ? 76 : undefined}
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
          const rss = formatRSS(rssForKey(item.option.value));
          const isSelected = item.option.value === selected;
          return (
            <Group gap="xs" wrap="nowrap" align="center" w="100%">
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
                  fontWeight: isSelected ? 700 : 400,
                }}
              >
                {item.option.label}
              </span>
              {rss ? (
                <Text size="xs" c="dimmed" ff="var(--mantine-font-family-monospace)">
                  {rss}
                </Text>
              ) : null}
            </Group>
          );
        }}
      />
      <Box
        mt="xs"
        p="sm"
        style={{
          background: "var(--mantine-color-dark-8)",
          borderRadius: "var(--mantine-radius-xs)",
          position: "relative",
        }}
      >
        <ScrollArea
          h={LOG_HEIGHT}
          viewportRef={viewportRef}
          onWheel={(e) => {
            if (e.deltaY < 0 || (viewportRef.current && !isNearBottom(viewportRef.current))) {
              pauseAutoFollow();
            }
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
