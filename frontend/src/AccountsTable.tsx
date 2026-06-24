import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ActionIcon,
  Badge,
  Box,
  Button,
  Checkbox,
  Group,
  PasswordInput,
  Table,
  TextInput,
  Tooltip,
} from "@mantine/core";
import { api, qk, type Account } from "./api";

type Draft = {
  source_user: string;
  source_password: string;
  dest_gmail: string;
  sync_checked: boolean;
};

const statusColor: Record<string, string> = {
  idle: "gray",
  running: "blue",
  ok: "green",
  failed: "red",
  skipped: "yellow",
  stopped: "orange",
};

export function AccountsTable({
  notify,
  onAuthUrl,
  running,
}: {
  notify: (color: string, msg: string) => void;
  onAuthUrl: (url: string, accountId: number) => void;
  running: boolean;
}) {
  const qc = useQueryClient();
  const { data: accounts } = useQuery({ queryKey: qk.accounts, queryFn: api.listAccounts });
  const [drafts, setDrafts] = useState<Record<number, Draft>>({});

  const invalidate = () => qc.invalidateQueries({ queryKey: qk.accounts });

  const setChecked = useMutation({
    mutationFn: (p: { id: number; checked: boolean }) => api.setChecked(p.id, p.checked),
    onSuccess: invalidate,
    onError: (e: Error) => notify("red", e.message),
  });
  const update = useMutation({
    mutationFn: (p: { id: number; a: Draft }) => api.updateAccount(p.id, p.a),
    onSuccess: () => {
      invalidate();
      notify("green", "Account saved");
    },
    onError: (e: Error) => notify("red", e.message),
  });
  const del = useMutation({
    mutationFn: (id: number) => api.deleteAccount(id),
    onSuccess: invalidate,
    onError: (e: Error) => notify("red", e.message),
  });
  const auth = useMutation({
    mutationFn: (id: number) => api.authURL(id),
    onSuccess: (d, id) => onAuthUrl(d.auth_url, id),
    onError: (e: Error) => notify("red", e.message),
  });
  const syncOne = useMutation({
    mutationFn: (id: number) => api.syncOne(id),
    onSuccess: () => notify("green", "Sync queued"),
    onError: (e: Error) => notify("red", e.message),
  });
  const checkAll = useMutation({ mutationFn: api.checkAll, onSuccess: invalidate });
  const checkNone = useMutation({ mutationFn: api.checkNone, onSuccess: invalidate });

  const startEdit = (a: Account) =>
    setDrafts((d) => ({
      ...d,
      [a.id]: {
        source_user: a.source_user,
        source_password: a.source_password,
        dest_gmail: a.dest_gmail,
        sync_checked: a.sync_checked,
      },
    }));
  const cancelEdit = (id: number) =>
    setDrafts((d) => {
      const n = { ...d };
      delete n[id];
      return n;
    });
  const saveEdit = (id: number) => {
    const d = drafts[id];
    if (!d) return;
    update.mutate({ id, a: d });
    cancelEdit(id);
  };
  const setDraft = (id: number, patch: Partial<Draft>) =>
    setDrafts((d) => ({ ...d, [id]: { ...d[id], ...patch } }));

  const dupCount = (accounts || []).filter((a) => a.duplicate).length;

  return (
    <Box>
      <Group mb="xs">
        <Button variant="default" size="xs" onClick={() => checkAll.mutate()}>
          Select All
        </Button>
        <Button variant="default" size="xs" onClick={() => checkNone.mutate()}>
          Select None
        </Button>
        {dupCount > 0 && (
          <Badge color="red">
            {dupCount} duplicate source{dupCount > 1 ? "s" : ""} — blocked from sync
          </Badge>
        )}
      </Group>
      <Table striped highlightOnHover withTableBorder>
        <Table.Thead>
          <Table.Tr>
            <Table.Th style={{ width: 40 }}>Sync</Table.Th>
            <Table.Th>Source User</Table.Th>
            <Table.Th style={{ width: 220 }}>Source Password</Table.Th>
            <Table.Th>Destination Gmail</Table.Th>
            <Table.Th style={{ width: 180 }}>Auth</Table.Th>
            <Table.Th style={{ width: 260 }}>Actions</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {(accounts || []).map((a) => {
            const d = drafts[a.id];
            return (
              <Table.Tr
                key={a.id}
                bg={a.duplicate ? "var(--mantine-color-red-1)" : undefined}
              >
                <Table.Td>
                  <Checkbox
                    checked={a.sync_checked}
                    onChange={(e) => setChecked.mutate({ id: a.id, checked: e.currentTarget.checked })}
                  />
                </Table.Td>
                <Table.Td>
                  {d ? (
                    <TextInput
                      size="xs"
                      value={d.source_user}
                      onChange={(e) => setDraft(a.id, { source_user: e.currentTarget.value })}
                    />
                  ) : (
                    a.source_user
                  )}
                </Table.Td>
                <Table.Td>
                  {d ? (
                    <PasswordInput
                      size="xs"
                      value={d.source_password}
                      onChange={(e) => setDraft(a.id, { source_password: e.currentTarget.value })}
                    />
                  ) : (
                    <PasswordInput size="xs" value={a.source_password} readOnly />
                  )}
                </Table.Td>
                <Table.Td>
                  {d ? (
                    <TextInput
                      size="xs"
                      value={d.dest_gmail}
                      onChange={(e) => setDraft(a.id, { dest_gmail: e.currentTarget.value })}
                    />
                  ) : (
                    a.dest_gmail
                  )}
                </Table.Td>
                <Table.Td>
                  <Badge color={a.authenticated ? "green" : "gray"} variant="light">
                    {a.authenticated ? "Authenticated" : "Not auth'd"}
                  </Badge>{" "}
                  <Badge color={statusColor[a.last_status] || "gray"} variant="light">
                    {a.last_status}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Group gap="xs">
                    {d ? (
                      <>
                        <Button size="xs" onClick={() => saveEdit(a.id)} loading={update.isPending}>
                          Save
                        </Button>
                        <Button size="xs" variant="default" onClick={() => cancelEdit(a.id)}>
                          Cancel
                        </Button>
                      </>
                    ) : (
                      <>
                        <Button size="xs" variant="light" onClick={() => auth.mutate(a.id)} loading={auth.isPending}>
                          Auth
                        </Button>
                        <Tooltip label={a.duplicate ? "Duplicate source — resolve first" : "Sync this account"}>
                          <Button
                            size="xs"
                            disabled={running || a.duplicate}
                            onClick={() => syncOne.mutate(a.id)}
                          >
                            Sync
                          </Button>
                        </Tooltip>
                        <Button size="xs" variant="default" onClick={() => startEdit(a)}>
                          Edit
                        </Button>
                        <ActionIcon color="red" variant="subtle" onClick={() => del.mutate(a.id)}>
                          ✕
                        </ActionIcon>
                      </>
                    )}
                  </Group>
                </Table.Td>
              </Table.Tr>
            );
          })}
          {(!accounts || accounts.length === 0) && (
            <Table.Tr>
              <Table.Td colSpan={6} style={{ textAlign: "center", opacity: 0.6 }}>
                No accounts yet. Use Add Row or Import.
              </Table.Td>
            </Table.Tr>
          )}
        </Table.Tbody>
      </Table>
    </Box>
  );
}
