import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type CreatedKey } from "../../api";
import { keys } from "../../queries";
import { Button, Card, ConfirmButton, CopyField, EmptyState, ErrorNote, Icon, inputClass, PageSpinner, RelativeTime } from "../../ui";
import { SettingsHeader } from "./Settings";

/** The plaintext key comes back once, to `onCreated`. */
export function CreateKeyForm({ defaultName = "", onCreated }: { defaultName?: string; onCreated: (k: CreatedKey) => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState(defaultName);
  const create = useMutation({
    mutationFn: () => api.keys.create(name.trim()),
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
        Keys let agents and backends use the REST API and the MCP server. A key can read and edit contacts, but it cannot manage keys, providers or the
        team.
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
                    </div>
                    <div className="text-xs text-muted">
                      Created <RelativeTime iso={k.created_at} /> · {k.last_used_at ? <>used <RelativeTime iso={k.last_used_at} /></> : "never used"}
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
