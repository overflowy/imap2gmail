import { useEffect, useRef, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { useQuery } from "@tanstack/react-query";
import { Box, Select, ScrollArea } from "@mantine/core";
import { api, qk } from "./api";

type LogMap = Record<string, string[]>;

function opKey(accountId: number, operationId: string) {
  return `${accountId}|${operationId}`;
}

export function OutputPane({
  logs,
  setLogs,
}: {
  logs: LogMap;
  setLogs: Dispatch<SetStateAction<LogMap>>;
}) {
  const { data: ops } = useQuery({ queryKey: qk.operations, queryFn: api.listOperations });
  const { data: accounts } = useQuery({ queryKey: qk.accounts, queryFn: api.listAccounts });
  const [selected, setSelected] = useState<string | null>(null);
  const endRef = useRef<HTMLDivElement>(null);

  const accountMap = new Map<number, string>();
  (accounts || []).forEach((a) => accountMap.set(a.id, a.source_user));

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

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [logs, selected]);

  const lines = selected ? logs[selected] || [] : [];
  const options = merged.map((o) => ({
    value: o.key,
    label: `${accountMap.get(o.accountId) || "#" + o.accountId} — ${o.opId}`,
  }));

  return (
    <Box>
      <Select
        label="Operation"
        placeholder="Select an operation"
        data={options}
        value={selected}
        onChange={setSelected}
        searchable
      />
      <Box
        mt="xs"
        style={{
          background: "#1a1b1e",
          borderRadius: 6,
          padding: 10,
          height: 380,
        }}
      >
        <ScrollArea h={360}>
          <pre
            style={{
              margin: 0,
              fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
              fontSize: 12,
              lineHeight: 1.4,
              color: "#d5d7db",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
            }}
          >
            {lines.length ? lines.join("\n") : "(no output yet — start a sync)"}
          </pre>
          <div ref={endRef} />
        </ScrollArea>
      </Box>
    </Box>
  );
}
