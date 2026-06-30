import { useEffect, useReducer, useRef, type MutableRefObject, type RefObject } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Anchor,
  Box,
  Button,
  Group,
  Indicator,
  Paper,
  Popover,
  ScrollArea,
  Select,
  Stack,
  Text,
} from "@mantine/core";
import { api, qk, type Account } from "./api";
import { mergeHistoricalAndLiveLines, splitLogContent, stripRSS } from "./logLines";
import { useSSE } from "./useSSE";

const LOG_HEIGHT = 550;

type LogMap = Record<string, string[]>;
type RssMap = Record<string, number>;
type OpStatus = "running" | "stopped" | "failed" | "ok";
type OperationEntry = { key: string; accountId: number; opId: string; rssBytes?: number };

type PaneState = {
  logs: LogMap;
  rssByKey: RssMap;
  selectedKey: string | null;
  removeOpen: boolean;
  pruneOpen: boolean;
};

type PaneAction =
  | { type: "appendLog"; key: string; line: string }
  | { type: "recordRSS"; key: string; rssBytes: number }
  | { type: "select"; key: string | null }
  | { type: "setRemoveOpen"; open: boolean }
  | { type: "setPruneOpen"; open: boolean }
  | { type: "removeLogSuccess"; key: string }
  | { type: "pruneLogsSuccess" };

const initialPaneState: PaneState = {
  logs: {},
  rssByKey: {},
  selectedKey: null,
  removeOpen: false,
  pruneOpen: false,
};

function paneReducer(state: PaneState, action: PaneAction): PaneState {
  switch (action.type) {
    case "appendLog":
      return {
        ...state,
        logs: { ...state.logs, [action.key]: [...(state.logs[action.key] || []), action.line] },
      };
    case "recordRSS":
      if (action.rssBytes <= (state.rssByKey[action.key] || 0)) return state;
      return { ...state, rssByKey: { ...state.rssByKey, [action.key]: action.rssBytes } };
    case "select":
      return { ...state, selectedKey: action.key };
    case "setRemoveOpen":
      return { ...state, removeOpen: action.open };
    case "setPruneOpen":
      return { ...state, pruneOpen: action.open };
    case "removeLogSuccess": {
      const logs = { ...state.logs };
      const rssByKey = { ...state.rssByKey };
      delete logs[action.key];
      delete rssByKey[action.key];
      return { ...state, logs, rssByKey, selectedKey: null, removeOpen: false };
    }
    case "pruneLogsSuccess":
      return { ...state, pruneOpen: false };
  }
}

function opKey(accountId: number, operationId: string) {
  return `${accountId}|${operationId}`;
}

function StatusMark({ status }: { status?: OpStatus }) {
  return (
    <Indicator
      color={status === "running" ? "yellow" : status === "ok" ? "green" : "red"}
      processing={status === "running"}
      disabled={!status}
      size={10}
      position="middle-start"
      offset={5}
    >
      <Box w={10} h={10} />
    </Indicator>
  );
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

function isNearBottom(el: HTMLElement) {
  return el.scrollHeight - el.scrollTop - el.clientHeight < 40;
}

function useOutputPaneModel({
  accounts,
  syncSelect,
}: {
  accounts: Account[];
  syncSelect: { accountId: number; token: number } | null;
}) {
  const { data: ops } = useQuery({ queryKey: qk.operations, queryFn: api.listOperations });
  const queryClient = useQueryClient();
  const [state, dispatch] = useReducer(paneReducer, initialPaneState);
  const viewportRef = useRef<HTMLDivElement>(null);
  const atBottomRef = useRef(true);
  const programmaticFollowRef = useRef(false);
  const renderedLogRef = useRef({ selected: null as string | null, version: "" });
  const autoSelectAccountId = useRef<number | null>(null);
  const lastSyncSelectTokenRef = useRef<number | null>(null);

  if (syncSelect && syncSelect.token !== lastSyncSelectTokenRef.current) {
    autoSelectAccountId.current = syncSelect.accountId;
    lastSyncSelectTokenRef.current = syncSelect.token;
  }

  const resetScroll = (behavior: ScrollBehavior = "auto") => {
    atBottomRef.current = true;
    programmaticFollowRef.current = behavior === "smooth";
    requestAnimationFrame(() => {
      const el = viewportRef.current;
      if (el) el.scrollTo({ top: el.scrollHeight, behavior });
    });
  };

  const selectOperation = (key: string | null, behavior: ScrollBehavior = "auto") => {
    dispatch({ type: "select", key });
    resetScroll(behavior);
  };

  const removeLog = useMutation({
    mutationFn: (v: { id: number; ts: string; key: string }) => api.deleteAccountLog(v.id, v.ts),
    onSuccess: (_data, v) => {
      queryClient.invalidateQueries({ queryKey: qk.operations });
      dispatch({ type: "removeLogSuccess", key: v.key });
    },
  });
  const pruneLogs = useMutation({
    mutationFn: () => api.pruneLogs(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: qk.operations });
      dispatch({ type: "pruneLogsSuccess" });
    },
  });

  useSSE(
    (accountId, operationId, line, rssBytes) => {
      const key = opKey(accountId, operationId);
      dispatch({ type: "appendLog", key, line: stripRSS(line) });
      if (rssBytes && rssBytes > 0) dispatch({ type: "recordRSS", key, rssBytes });
    },
    (accountId, operationId) => {
      if (autoSelectAccountId.current === accountId) {
        autoSelectAccountId.current = null;
        selectOperation(opKey(accountId, operationId));
      }
    },
  );

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

  const seen = new Set<string>();
  const merged: OperationEntry[] = [
    ...(ops || []).map((o): OperationEntry => ({
      key: opKey(o.account_id, o.operation_id),
      accountId: o.account_id,
      opId: o.operation_id,
      rssBytes: o.rss_bytes,
    })),
    ...Object.keys(state.logs).map((k) => {
      const [aid, oid] = k.split("|");
      return { key: k, accountId: Number(aid), opId: oid } satisfies OperationEntry;
    }),
  ]
    .filter((o) => (seen.has(o.key) ? false : (seen.add(o.key), true)))
    .sort((a, b) => b.opId.localeCompare(a.opId));

  const selected = state.selectedKey && merged.some((o) => o.key === state.selectedKey) ? state.selectedKey : merged[0]?.key ?? null;
  const selectedParts = selected?.split("|");
  const selectedAccountId = selectedParts ? Number(selectedParts[0]) : null;
  const selectedOperationId = selectedParts?.[1] ?? null;
  const persistedSelected = selected !== null && (ops || []).some((o) => opKey(o.account_id, o.operation_id) === selected);
  const { data: historicalLog } = useQuery({
    queryKey: ["account-log", selected],
    queryFn: () => api.getAccountLog(selectedAccountId!, selectedOperationId!),
    enabled: persistedSelected && selectedAccountId !== null && selectedOperationId !== null,
  });

  const accountById = new Map<number, Account>();
  accounts.forEach((a) => accountById.set(a.id, a));
  const newestOpByAccount = new Map<number, string>();
  for (const o of merged) {
    const cur = newestOpByAccount.get(o.accountId);
    if (cur === undefined || o.opId > cur) newestOpByAccount.set(o.accountId, o.opId);
  }
  const statusByKey = new Map<string, OpStatus>();
  for (const o of merged) {
    const a = accountById.get(o.accountId);
    if (!a || o.opId !== newestOpByAccount.get(o.accountId)) continue;
    if (a.last_status === "running" || a.last_status === "stopped" || a.last_status === "failed" || a.last_status === "ok") {
      statusByKey.set(o.key, a.last_status);
    }
  }

  const selStatus = selected ? statusByKey.get(selected) : undefined;
  const persistedRssByKey = new Map<string, number>();
  for (const o of merged) {
    if (o.rssBytes) persistedRssByKey.set(o.key, o.rssBytes);
  }
  const rssForKey = (key: string) => state.rssByKey[key] || persistedRssByKey.get(key) || 0;
  const selectedRSS = selected ? formatRSS(rssForKey(selected)) : "";
  const lines = selected
    ? mergeHistoricalAndLiveLines(historicalLog ? splitLogContent(historicalLog.content) : [], state.logs[selected] ?? [])
    : [];
  const logVersion = `${lines.length}|${lines[lines.length - 1] ?? ""}`;
  const options = merged.map((o) => ({
    value: o.key,
    label: `${accountMap.get(o.accountId) || "#" + o.accountId} — ${o.opId}`,
  }));

  return {
    atBottomRef,
    lines,
    logVersion,
    options,
    pauseAutoFollow,
    programmaticFollowRef,
    pruneLogs,
    pruneOpen: state.pruneOpen,
    removeLog,
    removeOpen: state.removeOpen,
    resetScroll,
    rssForKey,
    selected,
    selectedAccountId,
    selectedOperationId,
    selectedRSS,
    selectOperation,
    selStatus,
    setPruneOpen: (open: boolean) => dispatch({ type: "setPruneOpen", open }),
    setRemoveOpen: (open: boolean) => dispatch({ type: "setRemoveOpen", open }),
    statusByKey,
    viewportRef,
    renderedLogRef,
  };
}

function LogsToolbar({
  canRemove,
  onJump,
  onPrune,
  onRemove,
  pruneOpen,
  pruning,
  removeOpen,
  removing,
  setPruneOpen,
  setRemoveOpen,
}: {
  canRemove: boolean;
  onJump: () => void;
  onPrune: () => void;
  onRemove: () => void;
  pruneOpen: boolean;
  pruning: boolean;
  removeOpen: boolean;
  removing: boolean;
  setPruneOpen: (open: boolean) => void;
  setRemoveOpen: (open: boolean) => void;
}) {
  return (
    <Group justify="space-between" align="center" gap="xs" wrap="nowrap" mb={4}>
      <Text size="sm" fw={500}>
        Logs
      </Text>
      <Group gap="sm" align="center" wrap="nowrap">
        <Anchor component="button" type="button" size="xs" c="dimmed" onClick={onJump}>
          jump to latest
        </Anchor>
        <Popover
          opened={canRemove && removeOpen}
          onChange={(open) => setRemoveOpen(canRemove && open)}
          position="top-end"
          withArrow={false}
          shadow="md"
          trapFocus
        >
          <Popover.Target>
            <Anchor
              component="button"
              type="button"
              size="xs"
              c={canRemove ? "red" : "dimmed"}
              disabled={!canRemove}
              aria-disabled={!canRemove}
              tabIndex={canRemove ? 0 : -1}
              style={{ cursor: canRemove ? "pointer" : "not-allowed", opacity: canRemove ? 1 : 0.55 }}
              onClick={() => {
                if (canRemove) setRemoveOpen(!removeOpen);
              }}
            >
              remove log
            </Anchor>
          </Popover.Target>
          <Popover.Dropdown>
            <Stack gap="xs">
              <Text size="xs">Delete this log from the list and disk?</Text>
              <Group gap="xs" justify="flex-end">
                <Button size="xs" variant="default" onClick={() => setRemoveOpen(false)}>
                  Cancel
                </Button>
                <Button size="xs" color="red" loading={removing} disabled={!canRemove} onClick={onRemove}>
                  Remove
                </Button>
              </Group>
            </Stack>
          </Popover.Dropdown>
        </Popover>
        <Popover opened={pruneOpen} onChange={setPruneOpen} position="top-end" withArrow={false} shadow="md" trapFocus>
          <Popover.Target>
            <Anchor component="button" type="button" size="xs" c="red" onClick={() => setPruneOpen(!pruneOpen)}>
              prune older than 24h
            </Anchor>
          </Popover.Target>
          <Popover.Dropdown>
            <Stack gap="xs">
              <Text size="xs">Delete all logs older than 24 hours (every account)?</Text>
              <Group gap="xs" justify="flex-end">
                <Button size="xs" variant="default" onClick={() => setPruneOpen(false)}>
                  Cancel
                </Button>
                <Button size="xs" color="red" loading={pruning} onClick={onPrune}>
                  Prune
                </Button>
              </Group>
            </Stack>
          </Popover.Dropdown>
        </Popover>
      </Group>
    </Group>
  );
}

function OperationSelector({
  options,
  rssForKey,
  selected,
  selectedRSS,
  selStatus,
  statusByKey,
  onSelect,
}: {
  options: { value: string; label: string }[];
  rssForKey: (key: string) => number;
  selected: string | null;
  selectedRSS: string;
  selStatus?: OpStatus;
  statusByKey: Map<string, OpStatus>;
  onSelect: (key: string | null) => void;
}) {
  return (
    <Select
      placeholder="Select a log"
      data={options}
      value={selected}
      onChange={onSelect}
      searchable
      rightSection={
        selectedRSS ? (
          <Text size="xs" c="dimmed" ff="var(--mantine-font-family-monospace)">
            {selectedRSS}
          </Text>
        ) : undefined
      }
      rightSectionWidth={selectedRSS ? 76 : undefined}
      leftSection={selStatus ? <StatusMark status={selStatus} /> : undefined}
      renderOption={(item) => {
        const st = statusByKey.get(item.option.value);
        const rss = formatRSS(rssForKey(item.option.value));
        const isSelected = item.option.value === selected;
        return (
          <Group gap="xs" wrap="nowrap" align="center" w="100%">
            <StatusMark status={st} />
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
  );
}

function LogOutput({
  atBottomRef,
  lines,
  logVersion,
  pauseAutoFollow,
  programmaticFollowRef,
  renderedLogRef,
  selected,
  viewportRef,
}: {
  atBottomRef: MutableRefObject<boolean>;
  lines: string[];
  logVersion: string;
  pauseAutoFollow: () => void;
  programmaticFollowRef: MutableRefObject<boolean>;
  renderedLogRef: MutableRefObject<{ selected: string | null; version: string }>;
  selected: string | null;
  viewportRef: RefObject<HTMLDivElement | null>;
}) {
  return (
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
          if (e.deltaY < 0 || (viewportRef.current && !isNearBottom(viewportRef.current))) pauseAutoFollow();
        }}
      >
        <pre
          ref={(node) => {
            if (!node) return;
            const previous = renderedLogRef.current;
            const selectionChanged = previous.selected !== selected;
            const contentChanged = previous.version !== logVersion;
            renderedLogRef.current = { selected, version: logVersion };
            if (!selectionChanged && (!contentChanged || !atBottomRef.current)) return;

            atBottomRef.current = true;
            programmaticFollowRef.current = false;
            requestAnimationFrame(() => {
              const el = viewportRef.current;
              if (el) el.scrollTo({ top: el.scrollHeight, behavior: selectionChanged ? "auto" : "smooth" });
            });
          }}
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
  );
}

export function OutputPane({
  accounts,
  syncSelect,
}: {
  accounts: Account[];
  syncSelect: { accountId: number; token: number } | null;
}) {
  const model = useOutputPaneModel({ accounts, syncSelect });
  const removeSelectedLog = () => {
    if (model.selected && model.selStatus !== "running" && model.selectedAccountId !== null && model.selectedOperationId) {
      model.removeLog.mutate({ id: model.selectedAccountId, ts: model.selectedOperationId, key: model.selected });
    }
  };

  return (
    <Paper>
      <LogsToolbar
        canRemove={Boolean(model.selected && model.selStatus !== "running")}
        onJump={() => model.resetScroll("smooth")}
        onPrune={() => model.pruneLogs.mutate()}
        onRemove={removeSelectedLog}
        pruneOpen={model.pruneOpen}
        pruning={model.pruneLogs.isPending}
        removeOpen={model.removeOpen}
        removing={model.removeLog.isPending}
        setPruneOpen={model.setPruneOpen}
        setRemoveOpen={model.setRemoveOpen}
      />
      <OperationSelector
        options={model.options}
        rssForKey={model.rssForKey}
        selected={model.selected}
        selectedRSS={model.selectedRSS}
        selStatus={model.selStatus}
        statusByKey={model.statusByKey}
        onSelect={model.selectOperation}
      />
      <LogOutput
        atBottomRef={model.atBottomRef}
        lines={model.lines}
        logVersion={model.logVersion}
        pauseAutoFollow={model.pauseAutoFollow}
        programmaticFollowRef={model.programmaticFollowRef}
        renderedLogRef={model.renderedLogRef}
        selected={model.selected}
        viewportRef={model.viewportRef}
      />
    </Paper>
  );
}
