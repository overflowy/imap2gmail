import { useReducer, useRef } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Container,
  Group,
  Modal,
  PasswordInput,
  Stack,
  TextInput,
  Textarea,
  Title,
} from "@mantine/core";
import { api, qk } from "./api";
import { SettingsPanel } from "./SettingsPanel";
import { AccountsTable } from "./AccountsTable";
import { OutputPane } from "./OutputPane";

type Notice = { color: string; msg: string };

type AppState = {
  notice: Notice | null;
  addOpen: boolean;
  importOpen: boolean;
  importText: string;
  addForm: { source_user: string; source_password: string; dest_gmail: string };
  syncSelect: { accountId: number; token: number } | null;
};

type AppAction =
  | { type: "notice"; notice: Notice | null }
  | { type: "addOpen"; open: boolean }
  | { type: "importOpen"; open: boolean }
  | { type: "importText"; text: string }
  | { type: "addForm"; patch: Partial<AppState["addForm"]> }
  | { type: "resetAddForm" }
  | { type: "syncSelect"; value: AppState["syncSelect"] };

const emptyAddForm = { source_user: "", source_password: "", dest_gmail: "" };
const oauthCallbackNotice = readOAuthCallbackNotice();

function readOAuthCallbackNotice(): Notice | null {
  if (typeof window === "undefined") return null;
  const params = new URLSearchParams(window.location.search);
  const auth = params.get("auth");
  if (!auth) return null;
  window.history.replaceState({}, "", "/");
  if (auth === "ok") return { color: "green", msg: "Gmail authenticated successfully" };
  if (auth === "err") return { color: "red", msg: "Authentication failed: " + (params.get("info") || "unknown error") };
  return null;
}

function reducer(state: AppState, action: AppAction): AppState {
  switch (action.type) {
    case "notice":
      return { ...state, notice: action.notice };
    case "addOpen":
      return { ...state, addOpen: action.open };
    case "importOpen":
      return { ...state, importOpen: action.open };
    case "importText":
      return { ...state, importText: action.text };
    case "addForm":
      return { ...state, addForm: { ...state.addForm, ...action.patch } };
    case "resetAddForm":
      return { ...state, addForm: emptyAddForm };
    case "syncSelect":
      return { ...state, syncSelect: action.value };
  }
}

export default function App() {
  const queryClient = useQueryClient();
  const { data: accounts } = useQuery({ queryKey: qk.accounts, queryFn: api.listAccounts });
  const running = (accounts || []).some((a) => a.last_status === "running");

  const [state, dispatch] = useReducer(reducer, undefined, () => ({
    notice: oauthCallbackNotice,
    addOpen: false,
    importOpen: false,
    importText: "",
    addForm: emptyAddForm,
    syncSelect: null,
  }));
  // Monotonic token so re-syncing the same account still retriggers the
  // OutputPane auto-selection behavior (a fresh value each click).
  const syncTokenRef = useRef(0);

  const notify = (color: string, msg: string) => {
    dispatch({ type: "notice", notice: { color, msg } });
    window.setTimeout(() => dispatch({ type: "notice", notice: null }), 4000);
  };

  const syncAll = useMutation({
    mutationFn: api.syncAll,
    onSuccess: (d) => {
      queryClient.invalidateQueries({ queryKey: qk.accounts });
      queryClient.invalidateQueries({ queryKey: qk.operations });
      notify("green", `Sync started: ${d.queued} queued, ${d.skipped} skipped`);
    },
    onError: (e: Error) => notify("red", e.message),
  });
  const stop = useMutation({
    mutationFn: api.stop,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: qk.accounts });
      queryClient.invalidateQueries({ queryKey: qk.operations });
      notify("orange", "Stopping all active syncs...");
    },
    onError: (e: Error) => notify("red", e.message),
  });
  const addAccount = useMutation({
    mutationFn: () => api.addAccount(state.addForm),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: qk.accounts });
      dispatch({ type: "addOpen", open: false });
      dispatch({ type: "resetAddForm" });
      notify("green", "Account added");
    },
    onError: (e: Error) => notify("red", e.message),
  });
  const doImport = useMutation({
    mutationFn: () => api.importAccounts(state.importText),
    onSuccess: (d) => {
      queryClient.invalidateQueries({ queryKey: qk.accounts });
      dispatch({ type: "importOpen", open: false });
      dispatch({ type: "importText", text: "" });
      notify("green", `Imported ${d.imported} account${d.imported === 1 ? "" : "s"}${d.skipped ? `, skipped ${d.skipped}` : ""}`);
    },
    onError: (e: Error) => notify("red", e.message),
  });

  return (
    <Container size="xl" py="md">
      <Stack gap="md">
        <Group justify="space-between" align="center">
          <Title order={3}>imap2gmail</Title>
          <Group>
            <Button
              color="indigo"
              loading={syncAll.isPending}
              disabled={running}
              onClick={() => {
                const first = (accounts || []).find((a) => a.sync_checked && !a.duplicate);
                if (first) dispatch({ type: "syncSelect", value: { accountId: first.id, token: ++syncTokenRef.current } });
                syncAll.mutate();
              }}
            >
              Sync All
            </Button>
            <Button color="red" variant="light" disabled={!running} onClick={() => stop.mutate()}>
              Stop
            </Button>
          </Group>
        </Group>

        {state.notice && (
          <Alert color={state.notice.color} withCloseButton onClose={() => dispatch({ type: "notice", notice: null })}>
            {state.notice.msg}
          </Alert>
        )}

        <SettingsPanel notify={notify} />

        <AccountsTable
          accounts={accounts ?? []}
          running={running}
          notify={notify}
          onAdd={() => dispatch({ type: "addOpen", open: true })}
          onImport={() => dispatch({ type: "importOpen", open: true })}
          onSyncStart={(id) => dispatch({ type: "syncSelect", value: { accountId: id, token: ++syncTokenRef.current } })}
        />

        <OutputPane accounts={accounts ?? []} syncSelect={state.syncSelect} />
      </Stack>

      {/* Add Row modal */}
      <Modal opened={state.addOpen} onClose={() => dispatch({ type: "addOpen", open: false })} title="Add Account">
        <Stack>
          <TextInput
            label="Source User"
            value={state.addForm.source_user}
            onChange={(e) => dispatch({ type: "addForm", patch: { source_user: e.currentTarget.value } })}
          />
          <PasswordInput
            label="Source Password"
            value={state.addForm.source_password}
            onChange={(e) => dispatch({ type: "addForm", patch: { source_password: e.currentTarget.value } })}
          />
          <TextInput
            label="Destination Gmail"
            value={state.addForm.dest_gmail}
            onChange={(e) => dispatch({ type: "addForm", patch: { dest_gmail: e.currentTarget.value } })}
          />
          <Button loading={addAccount.isPending} onClick={() => addAccount.mutate()}>
            Add
          </Button>
        </Stack>
      </Modal>

      {/* Import modal */}
      <Modal opened={state.importOpen} onClose={() => dispatch({ type: "importOpen", open: false })} title="Import Accounts">
        <Stack>
          <Textarea
            autosize
            minRows={8}
            value={state.importText}
            onChange={(e) => dispatch({ type: "importText", text: e.currentTarget.value })}
            placeholder={"source_user,password,gmail\nalice,secret1,alice@gmail.com\nbob,secret2,bob@gmail.com"}
            styles={{ input: { fontFamily: "var(--mantine-font-family-monospace)" } }}
          />
          <Button loading={doImport.isPending} onClick={() => doImport.mutate()}>
            Import
          </Button>
        </Stack>
      </Modal>
    </Container>
  );
}
