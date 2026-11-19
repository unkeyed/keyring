import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type CreatedKey } from "../../api";
import { keys } from "../../queries";
import { Button, Card, ConfirmButton, CopyField, ErrorNote, Icon, inputClass, RelativeTime, Spinner } from "../../ui";
import { SettingsHeader } from "./Settings";

/** Names a key and creates it. The plaintext comes back once, to `onCreated`. */
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
      <div className="flex gap-2">
        <input aria-label="Key name" placeholder="What will use it? e.g. Claude Code, signup backend" value={name} onChange={(e) => setName(e.target.value)} className={inputClass} />
        <Button type="submit" variant="primary" disabled={!name.trim() || create.isPending} className="shrink-0">
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
    <div className="rounded-md border border-warn/40 bg-warn-soft p-3">
      <div className="mb-1.5 flex items-center justify-between gap-2 text-xs font-medium text-warn">
        <span>“{k.name}” created. Copy the key now; it is shown only once.</span>
        {onDone && (
          <Button size="sm" variant="ghost" onClick={onDone}>
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

      <Card className="mt-6">
        {list.isPending ? (
          <div className="p-4">
            <Spinner />
          </div>
        ) : list.data?.length === 0 ? (
          <p className="p-4 text-[13px] text-faint">No keys yet.</p>
        ) : (
          <table className="w-full text-[13px]" aria-label="API keys">
            <thead>
              <tr className="border-b border-line text-left text-xs text-muted">
                <th scope="col" className="px-4 py-2 font-medium">Name</th>
                <th scope="col" className="px-3 py-2 font-medium">Key</th>
                <th scope="col" className="px-3 py-2 font-medium">Created</th>
                <th scope="col" className="px-3 py-2 font-medium">Last used</th>
                <th scope="col" className="px-4 py-2">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {list.data?.map((k) => (
                <tr key={k.id} className="border-b border-line last:border-0">
                  <td className="px-4 py-2 font-medium">{k.name}</td>
                  <td className="px-3 py-2 font-mono text-xs text-muted">{k.prefix}…</td>
                  <td className="px-3 py-2 text-muted">
                    <RelativeTime iso={k.created_at} />
                  </td>
                  <td className="px-3 py-2 text-muted">{k.last_used_at ? <RelativeTime iso={k.last_used_at} /> : <span className="text-faint">Never</span>}</td>
                  <td className="px-4 py-2 text-right">
                    <ConfirmButton label={`Revoke ${k.name}`} confirmLabel="Revoke?" onConfirm={() => revoke.mutate(k.id)} disabled={revoke.isPending}>
                      Revoke
                    </ConfirmButton>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
      <div className="mt-2">
        <ErrorNote error={list.error ?? revoke.error} />
      </div>
    </>
  );
}
