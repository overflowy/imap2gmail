import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { qk } from "./api";

type SSEEvent = {
  type: string;
  account_id?: number;
  source_user?: string;
  operation_id?: string;
  line?: string;
  status?: string;
  reason?: string;
  dest_gmail?: string;
};

/**
 * useSSE maintains a single global EventSource to /events. It appends log lines
 * via onLog and invalidates TanStack Query caches on status / auth-ok / operation
 * events so the table and operation selector stay in sync without polling.
 */
export function useSSE(
  onLog: (accountId: number, operationId: string, line: string) => void
) {
  const qc = useQueryClient();
  const onLogRef = useRef(onLog);
  onLogRef.current = onLog;

  useEffect(() => {
    const es = new EventSource("/events");
    const dispatch = (ev: SSEEvent) => {
      switch (ev.type) {
        case "log":
          if (ev.operation_id && ev.line != null && ev.account_id != null) {
            onLogRef.current(ev.account_id, ev.operation_id, ev.line);
          }
          break;
        case "status":
          qc.invalidateQueries({ queryKey: qk.accounts });
          break;
        case "auth-ok":
          qc.invalidateQueries({ queryKey: qk.accounts });
          break;
        case "operation":
          qc.invalidateQueries({ queryKey: qk.operations });
          qc.invalidateQueries({ queryKey: qk.accounts });
          break;
      }
    };
    const add = (e: MessageEvent) => {
      try {
        dispatch(JSON.parse(e.data));
      } catch {
        /* ignore malformed */
      }
    };
    ["log", "status", "auth-ok", "operation", "hello"].forEach((t) =>
      es.addEventListener(t, add as EventListener)
    );
    return () => es.close();
  }, [qc]);
}
