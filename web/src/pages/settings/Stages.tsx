import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Stage } from "../../api";
import { keys, useStages } from "../../queries";
import { Button, Card, cx, ErrorNote, Icon, inputClass, Spinner } from "../../ui";
import { SettingsHeader } from "./Settings";

/** A row remembers the name it was loaded with, so a rename can carry contacts along. */
interface Row {
  key: number;
  original: Stage | null;
  name: string;
}

let nextKey = 0;
const toRows = (stages: Stage[]): Row[] => stages.map((s) => ({ key: nextKey++, original: s, name: s }));

export function Stages() {
  const stages = useStages();
  if (stages.isPending) return <Spinner />;
  if (stages.isError) return <ErrorNote error={stages.error} />;
  return <Editor key={stages.dataUpdatedAt} initial={stages.data} />;
}

function Editor({ initial }: { initial: Stage[] }) {
  const qc = useQueryClient();
  const [rows, setRows] = useState(() => toRows(initial));
  const [draft, setDraft] = useState("");

  const names = rows.map((r) => r.name.trim());
  const dirty = names.join("\n") !== initial.join("\n");
  const duplicate = names.find((n, i) => names.indexOf(n) !== i);
  const blank = names.some((n) => !n);

  const save = useMutation({
    mutationFn: () => {
      const renames: Record<Stage, Stage> = {};
      for (const r of rows) if (r.original && r.original !== r.name.trim()) renames[r.original] = r.name.trim();
      return api.stages.set(names, renames);
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.contacts });
      return qc.invalidateQueries({ queryKey: keys.stages });
    },
  });

  const move = (i: number, by: number) => {
    const next = [...rows];
    [next[i], next[i + by]] = [next[i + by], next[i]];
    setRows(next);
  };
  const add = (e: FormEvent) => {
    e.preventDefault();
    if (!draft.trim()) return;
    setRows([...rows, { key: nextKey++, original: null, name: draft.trim() }]);
    setDraft("");
  };

  return (
    <>
      <SettingsHeader title="Stages">
        The pipeline every contact moves through, in order. Renaming a stage moves its contacts with it.
      </SettingsHeader>
      <Card>
        <ol aria-label="Stages">
          {rows.map((r, i) => (
            <li key={r.key} className="flex items-center gap-2 border-b border-line px-3 py-2 last:border-0">
              <span className="w-5 text-right text-xs text-faint tabular-nums">{i + 1}</span>
              <input
                aria-label={`Stage ${i + 1} name`}
                value={r.name}
                onChange={(e) => setRows(rows.map((x) => (x.key === r.key ? { ...x, name: e.target.value } : x)))}
                className={cx(inputClass, "flex-1")}
              />
              {r.original && r.original !== r.name.trim() && <span className="text-xs text-faint">was {r.original}</span>}
              <Button size="sm" variant="ghost" aria-label={`Move ${r.name} up`} disabled={i === 0} onClick={() => move(i, -1)}>
                <Icon name="up" className="size-3.5" />
              </Button>
              <Button size="sm" variant="ghost" aria-label={`Move ${r.name} down`} disabled={i === rows.length - 1} onClick={() => move(i, 1)}>
                <Icon name="down" className="size-3.5" />
              </Button>
              <Button size="sm" variant="ghost" aria-label={`Remove ${r.name}`} disabled={rows.length === 1} onClick={() => setRows(rows.filter((x) => x.key !== r.key))}>
                <Icon name="trash" className="size-3.5" />
              </Button>
            </li>
          ))}
        </ol>
        <form onSubmit={add} className="flex gap-2 border-t border-line px-3 py-2">
          <span className="w-5" />
          <input aria-label="New stage" placeholder="Add a stage" value={draft} onChange={(e) => setDraft(e.target.value)} className={inputClass} />
          <Button type="submit" size="md" disabled={!draft.trim()}>
            Add
          </Button>
        </form>
      </Card>
      <div className="mt-3 space-y-3">
        {duplicate && <p className="text-xs text-danger">“{duplicate}” is listed twice.</p>}
        <ErrorNote error={save.error} />
        <div className="flex gap-2">
          <Button variant="primary" disabled={!dirty || !!duplicate || blank || save.isPending} onClick={() => save.mutate()}>
            {save.isPending ? "Saving…" : "Save stages"}
          </Button>
          {dirty && (
            <Button variant="ghost" onClick={() => setRows(toRows(initial))}>
              Discard changes
            </Button>
          )}
        </div>
        <p className="text-xs text-faint">A stage that still has contacts can&apos;t be removed. Move them first, or rename the stage instead.</p>
      </div>
    </>
  );
}
