import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useReauthMutation } from "./Reauth";
import { api, type CreatedKey, type Key } from "../../api";
import { keys } from "../../queries";
import { Badge, Button, Card, ConfirmButton, CopyField, EmptyState, ErrorNote, Icon, inputClass, PageSpinner, RelativeTime, selectClass } from "../../ui";
import { SettingsHeader } from "./Settings";

/** Lifetimes offered when creating a key, in days; 0 is until revoked. */
const lifetimes = [
  { days: 30, label: "Expires in 30 days" },
  { days: 90, label: "Expires in 90 days" },
  { days: 365, label: "Expires in 1 year" },
  { days: 0, label: "Never expires" },
];

function expiresAt(days: number): string | undefined {
  return days ? new Date(Date.now() + days * 86_400_000).toISOString() : undefined;
}

/** The plaintext key comes back once, to `onCreated`. */
export function CreateKeyForm({
  defaultName = "",
  defaultDays = 90,
  allowReadOnly = true,
  onCreated,
}: {
  defaultName?: string;
  /** The lifetime selected at first. A key a backend relies on defaults to 0, never expiring. */
  defaultDays?: number;
  /** False where the key must write, such as a signup backend. */
  allowReadOnly?: boolean;
  onCreated: (k: CreatedKey) => void;
}) {
  const qc = useQueryClient();
  const [name, setName] = useState(defaultName);
  const [days, setDays] = useState(defaultDays);
  const [readOnly, setReadOnly] = useState(false);
  const create = useReauthMutation({
    mutationFn: () => api.keys.create({ name: name.trim(), read_only: readOnly, expires_at: expiresAt(days) }),
    onSuccess: (k) => {
      setName(defaultName);
      onCreated(k);
      return qc.invalidateQueries({ queryKey: keys.keys });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (name.trim()) create.mutate();
  };
  return (
    <form onSubmit={submit} className="space-y-2">
      <div className="flex flex-col gap-2 sm:flex-row">
        <input aria-label="Key name" placeholder="What will use it? Like Claude Code or a signup backend" value={name} onChange={(e) => setName(e.target.value)} className={inputClass} />
        <Button type="submit" variant="primary" disabled={!name.trim() || create.isPending}>
          <Icon name="plus" className="size-3.5" />
          Create key
        </Button>
      </div>
      <div className="flex flex-col gap-2 sm:flex-row">
        {allowReadOnly && (
          <select aria-label="Access" className={selectClass} value={readOnly ? "read" : "write"} onChange={(e) => setReadOnly(e.target.value === "read")}>
            <option value="write">Read and edit</option>
            <option value="read">Read only</option>
          </select>
        )}
        <select aria-label="Expiry" className={selectClass} value={days} onChange={(e) => setDays(Number(e.target.value))}>
          {lifetimes.map((l) => (
            <option key={l.days} value={l.days}>
              {l.label}
            </option>
          ))}
        </select>
      </div>
      <ErrorNote error={create.error} />
    </form>
  );
}

export function NewKey({ k, onDone }: { k: CreatedKey; onDone?: () => void }) {
  return (
    <div className="rounded-surface border border-line-strong bg-surface p-3">
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="min-w-0 text-sm">
          <span className="font-medium [overflow-wrap:anywhere]">“{k.name}” created.</span> <span className="text-muted">Copy the key now. It is shown only once.</span>
        </div>
        {onDone && (
          <Button size="sm" variant="ghost" onClick={onDone} className="-my-1">
            Done
          </Button>
        )}
      </div>
      <CopyField value={k.key} />
    </div>
  );
}

export function ApiKeys() {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: keys.keys, queryFn: async () => (await api.keys.list()) ?? [] });
  const [created, setCreated] = useState<CreatedKey | null>(null);
  const revoke = useMutation({
    mutationFn: api.keys.revoke,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.keys }),
  });

  return (
    <>
      <SettingsHeader title="API keys">
        Keys let agents and backends use the REST API. A key can read and edit contacts, or only read them, but it cannot manage keys, providers or
        the team. Give each key an expiry unless something you cannot easily update depends on it. MCP clients sign in as a person instead, under
        Agents &amp; MCP.
      </SettingsHeader>
      <div className="space-y-3">
        <CreateKeyForm onCreated={setCreated} />
        {created && <NewKey k={created} onDone={() => setCreated(null)} />}
      </div>

      <div className="mt-6">
        {list.isPending ? (
          <PageSpinner />
        ) : list.data?.length === 0 ? (
          <EmptyState icon="key" title="No keys yet">
            Create one above for each agent or backend, so you can revoke them one at a time.
          </EmptyState>
        ) : (
          <Card>
            <ul aria-label="API keys" className="divide-y divide-line">
              {list.data?.map((k) => (
                <li key={k.id} className="flex items-center gap-3 px-4 py-3">
                  <span className="grid size-8 shrink-0 place-items-center rounded-control bg-subtle text-muted">
                    <Icon name="key" className="size-4" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
                      <span className="min-w-0 truncate font-medium" title={k.name}>
                        {k.name}
                      </span>
                      <code className="font-mono text-xs text-muted">{k.prefix}…</code>
                      {k.read_only && <Badge tone="info">Read only</Badge>}
                      <KeyExpiry k={k} />
                    </div>
                    <div className="text-xs text-muted">
                      Created <RelativeTime iso={k.created_at} /> · {k.last_used_at ? <>used <RelativeTime iso={k.last_used_at} /></> : "never used"}
                      {k.expires_at && (
                        <>
                          {" "}
                          · {new Date(k.expires_at) <= new Date() ? "expired" : "expires"} <RelativeTime iso={k.expires_at} />
                        </>
                      )}
                    </div>
                  </div>
                  <ConfirmButton label={`Revoke ${k.name}`} confirmLabel="Revoke?" onConfirm={() => revoke.mutate(k.id)} disabled={revoke.isPending}>
                    Revoke
                  </ConfirmButton>
                </li>
              ))}
            </ul>
          </Card>
        )}
      </div>
      <div className="mt-2 empty:hidden">
        <ErrorNote error={list.error ?? revoke.error} />
      </div>
    </>
  );
}

function KeyExpiry({ k }: { k: Key }) {
  if (!k.expires_at) return null;
  const left = new Date(k.expires_at).getTime() - Date.now();
  if (left <= 0) return <Badge tone="danger">Expired</Badge>;
  if (left < 7 * 86_400_000) return <Badge tone="warn">Expires soon</Badge>;
  return null;
}
