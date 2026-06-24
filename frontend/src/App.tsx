import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Container,
  CopyButton,
  Group,
  Modal,
  Stack,
  TextInput,
  Textarea,
  Title,
} from "@mantine/core";
import { api, qk } from "./api";
import { useSSE } from "./useSSE";
import { SettingsPanel } from "./SettingsPanel";
import { AccountsTable } from "./AccountsTable";
import { OutputPane } from "./OutputPane";

type LogMap = Record<string, string[]>;
type Notice = { color: string; msg: string };

export default function App() {
  const qc = useQueryClient();
  const { data: accounts } = useQuery({ queryKey: qk.accounts, queryFn: api.listAccounts });
  const running = (accounts || []).some((a) => a.last_status === "running");

  const [notice, setNotice] = useState<Notice | null>(null);
  const [logs, setLogs] = useState<LogMap>({});
  const [authUrl, setAuthUrl] = useState<{ url: string; accountId: number } | null>(null);
  const [pasteCode, setPasteCode] = useState("");
  const [addOpen, setAddOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [importText, setImportText] = useState("");
  const [addForm, setAddForm] = useState({ source_user: "", source_password: "", dest_gmail: "" });

  const notify = (color: string, msg: string) => {
    setNotice({ color, msg });
    window.setTimeout(() => setNotice(null), 4000);
  };

  // Single global SSE connection: append live log lines + invalidate caches.
  useSSE((accountId, operationId, line) => {
    const key = `${accountId}|${operationId}`;
    setLogs((prev) => ({ ...prev, [key]: [...(prev[key] || []), line] }));
  });

  // OAuth callback feedback (?auth=ok|err set by the redirect).
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const auth = params.get("auth");
    if (auth === "ok") notify("green", "Gmail authenticated successfully");
    else if (auth === "err") notify("red", "Authentication failed: " + (params.get("info") || "unknown error"));
    if (auth) window.history.replaceState({}, "", "/");
  }, []);

  const syncAll = useMutation({
    mutationFn: api.syncAll,
    onSuccess: (d) => notify("green", `Sync started: ${d.queued} queued, ${d.skipped} skipped`),
    onError: (e: Error) => notify("red", e.message),
  });
  const stop = useMutation({
    mutationFn: api.stop,
    onSuccess: () => notify("orange", "Stopping all active syncs…"),
    onError: (e: Error) => notify("red", e.message),
  });
  const addAccount = useMutation({
    mutationFn: () => api.addAccount(addForm),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.accounts });
      setAddOpen(false);
      setAddForm({ source_user: "", source_password: "", dest_gmail: "" });
      notify("green", "Account added");
    },
    onError: (e: Error) => notify("red", e.message),
  });
  const doImport = useMutation({
    mutationFn: () => api.importAccounts(importText),
    onSuccess: (d) => {
      qc.invalidateQueries({ queryKey: qk.accounts });
      setImportOpen(false);
      setImportText("");
      notify("green", `Imported ${d.imported} account${d.imported === 1 ? "" : "s"}${d.skipped ? `, skipped ${d.skipped}` : ""}`);
    },
    onError: (e: Error) => notify("red", e.message),
  });
  const exchange = useMutation({
    mutationFn: () => api.authExchange(authUrl!.accountId, pasteCode),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.accounts });
      setPasteCode("");
      notify("green", "Tokens exchanged");
    },
    onError: (e: Error) => notify("red", e.message),
  });

  return (
    <Container size="xl" py="md">
      <Stack gap="md">
        <Group justify="space-between">
          <Title order={3}>imap2gmail</Title>
          <Group>
            <Button
              color="indigo"
              loading={syncAll.isPending}
              disabled={running}
              onClick={() => syncAll.mutate()}
            >
              Sync All
            </Button>
            <Button color="red" variant="light" disabled={!running} onClick={() => stop.mutate()}>
              Stop
            </Button>
          </Group>
        </Group>

        {notice && (
          <Alert color={notice.color} withCloseButton onClose={() => setNotice(null)}>
            {notice.msg}
          </Alert>
        )}

        <SettingsPanel notify={notify} />

        {/* Auth URL bar */}
        {authUrl && (
          <Stack gap="xs">
            <Group align="flex-end">
              <TextInput
                label="Auth URL (open it to authorize Gmail)"
                readOnly
                value={authUrl.url}
                style={{ flex: 1 }}
              />
              <CopyButton value={authUrl.url}>
                {({ copied }) => (
                  <Button color={copied ? "teal" : "indigo"}>{copied ? "Copied" : "Copy"}</Button>
                )}
              </CopyButton>
            </Group>
            <Group align="flex-end">
              <TextInput
                placeholder="…or paste the authorization code here (manual fallback)"
                value={pasteCode}
                onChange={(e) => setPasteCode(e.currentTarget.value)}
                style={{ flex: 1 }}
              />
              <Button
                variant="default"
                disabled={!pasteCode}
                loading={exchange.isPending}
                onClick={() => exchange.mutate()}
              >
                Exchange Code
              </Button>
            </Group>
          </Stack>
        )}

        {/* Toolbar */}
        <Group>
          <Button variant="default" onClick={() => setAddOpen(true)}>
            Add Row
          </Button>
          <Button variant="default" onClick={() => setImportOpen(true)}>
            Import
          </Button>
        </Group>

        <AccountsTable
          notify={notify}
          running={running}
          onAuthUrl={(url, accountId) => setAuthUrl({ url, accountId })}
        />

        <OutputPane logs={logs} setLogs={setLogs} />
      </Stack>

      {/* Add Row modal */}
      <Modal opened={addOpen} onClose={() => setAddOpen(false)} title="Add Account">
        <Stack>
          <TextInput
            label="Source User"
            value={addForm.source_user}
            onChange={(e) => setAddForm({ ...addForm, source_user: e.currentTarget.value })}
          />
          <TextInput
            label="Source Password"
            value={addForm.source_password}
            onChange={(e) => setAddForm({ ...addForm, source_password: e.currentTarget.value })}
          />
          <TextInput
            label="Destination Gmail"
            value={addForm.dest_gmail}
            onChange={(e) => setAddForm({ ...addForm, dest_gmail: e.currentTarget.value })}
          />
          <Button loading={addAccount.isPending} onClick={() => addAccount.mutate()}>
            Add
          </Button>
        </Stack>
      </Modal>

      {/* Import modal */}
      <Modal opened={importOpen} onClose={() => setImportOpen(false)} title="Import Accounts">
        <Stack>
          <Textarea
            label="Paste CSV (source_user,password,gmail) — comma or tab separated"
            autosize
            minRows={8}
            value={importText}
            onChange={(e) => setImportText(e.currentTarget.value)}
            placeholder={"source_user,password,gmail\nalice,secret1,alice@gmail.com\nbob,secret2,bob@gmail.com"}
          />
          <Button loading={doImport.isPending} onClick={() => doImport.mutate()}>
            Import
          </Button>
        </Stack>
      </Modal>
    </Container>
  );
}
