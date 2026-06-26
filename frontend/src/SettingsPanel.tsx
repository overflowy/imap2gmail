import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Anchor,
  Button,
  Collapse,
  Group,
  NumberInput,
  Paper,
  PasswordInput,
  Stack,
  Switch,
  Textarea,
  TextInput,
} from "@mantine/core";
import { api, qk, type Settings } from "./api";

export function SettingsPanel({ notify }: { notify: (color: string, msg: string) => void }) {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: qk.settings, queryFn: api.getSettings });
  const [open, setOpen] = useState(() => {
    try {
      return localStorage.getItem("imap2gmail:settingsOpen") !== "false";
    } catch {
      return true;
    }
  });
  const [form, setForm] = useState<Settings | null>(null);

  useEffect(() => {
    if (data) setForm(data);
  }, [data]);

  useEffect(() => {
    try {
      localStorage.setItem("imap2gmail:settingsOpen", String(open));
    } catch {
      /* ignore quota / disabled storage */
    }
  }, [open]);

  const save = useMutation({
    mutationFn: (s: Settings) => api.saveSettings(s),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.settings });
      notify("green", "Settings saved");
    },
    onError: (e: Error) => notify("red", e.message),
  });

  if (!form) return null;
  const set = (patch: Partial<Settings>) => setForm({ ...form, ...patch });

  return (
    <Paper>
      <Group justify="space-between" align="center" mb="xs">
        <Button variant="subtle" onClick={() => setOpen((o) => !o)}>
          {open ? "▾" : "▸"} Settings
        </Button>
      </Group>
      <Collapse expanded={open}>
        <Stack gap="md">
          <Group grow>
            <TextInput
              label="OAuth Client ID"
              value={form.client_id}
              onChange={(e) => set({ client_id: e.currentTarget.value })}
            />
            <PasswordInput
              label="OAuth Client Secret"
              value={form.client_secret}
              onChange={(e) => set({ client_secret: e.currentTarget.value })}
            />
          </Group>
          <Group grow align="flex-end">
            <TextInput
              label="Origin Host"
              value={form.origin_host}
              onChange={(e) => set({ origin_host: e.currentTarget.value })}
            />
            <NumberInput
              label="Origin Port"
              value={form.origin_port}
              onChange={(v) => set({ origin_port: Number(v) || 0 })}
            />
            <Switch
              label="Origin SSL"
              style={{
                alignSelf: "flex-end",
                height: "calc(2.25rem * var(--mantine-scale))",
                display: "flex",
                alignItems: "center",
              }}
              checked={form.origin_ssl}
              onChange={(e) => set({ origin_ssl: e.currentTarget.checked })}
            />
          </Group>
          <Textarea
            label={
              <Group justify="space-between" align="center" gap="xs" wrap="nowrap">
                <span>imapsync flags (global)</span>
                <Anchor
                  component="button"
                  type="button"
                  size="xs"
                  c="dimmed"
                  onClick={() =>
                    set({
                      imapsync_flags: form.default_imapsync_flags,
                    })
                  }
                >
                  reset
                </Anchor>
              </Group>
            }
            autosize
            minRows={2}
            value={form.imapsync_flags}
            onChange={(e) => set({ imapsync_flags: e.currentTarget.value })}
          />
          <Group grow align="flex-end">
            <NumberInput
              label="Bind Port"
              value={form.bind_port}
              onChange={(v) => set({ bind_port: Number(v) || 0 })}
            />
            <NumberInput
              label="Max Concurrent (1-8)"
              min={1}
              max={8}
              value={form.max_concurrent}
              onChange={(v) => set({ max_concurrent: Number(v) || 1 })}
            />
            <Switch
              label="Dry Run"
              style={{
                alignSelf: "flex-end",
                height: "calc(2.25rem * var(--mantine-scale))",
                display: "flex",
                alignItems: "center",
              }}
              checked={form.dry_run}
              onChange={(e) => set({ dry_run: e.currentTarget.checked })}
            />
          </Group>
          <TextInput label="Redirect URL (read-only)" readOnly value={form.redirect_url} />
          <Alert color="yellow" p="xs">
            Changing bind_port requires re-registering this URL in Google Console → Authorized
            redirect URIs, then restarting the app (the server only rebinds on restart).
          </Alert>
          <Group justify="flex-end">
            <Button loading={save.isPending} onClick={() => save.mutate(form)}>
              Save Settings
            </Button>
          </Group>
        </Stack>
      </Collapse>
    </Paper>
  );
}
