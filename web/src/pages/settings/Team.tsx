import { useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api, inviteURL, type Email } from "../../api";
import { keys, useMe } from "../../queries";
import { Avatar, Badge, Button, Card, CopyField, ErrorNote, Field, inputClass, PageSpinner, RelativeTime } from "../../ui";
import { SettingsHeader, SettingsSubhead } from "./Settings";

/** An invite made on this page. The API cannot list invites, so only these are shown. */
interface SentInvite {
  email: Email;
  url: string;
}

export function Team() {
  const [sent, setSent] = useState<SentInvite[]>([]);
  return (
    <>
      <SettingsHeader title="Team">Everyone on the team can log in, and see and edit every contact. Invite a teammate with a one-time link.</SettingsHeader>
      <Members />

      <SettingsSubhead className="mt-10">Invite a teammate</SettingsSubhead>
      <Invite onSent={(i) => setSent([i, ...sent])} />
      {sent.length > 0 && (
        <Card className="mt-3">
          <ul aria-label="Pending invites" className="divide-y divide-line">
            {sent.map((i) => (
              <li key={i.url} className="space-y-2 px-4 py-3">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="min-w-0 font-medium [overflow-wrap:anywhere]">{i.email}</span>
                  <Badge tone="info">Invite pending</Badge>
                </div>
                <CopyField value={i.url} label="Copy link" />
                <p className="text-xs text-muted">Send this link to them. It works once and expires in 7 days. It is not shown again after you leave this page.</p>
              </li>
            ))}
          </ul>
        </Card>
      )}

      <SettingsSubhead className="mt-10">Your password</SettingsSubhead>
      <Card className="p-4">
        <ChangePassword />
      </Card>
    </>
  );
}

function Members() {
  const me = useMe();
  const users = useQuery({ queryKey: keys.users, queryFn: async () => (await api.users.list()) ?? [] });
  if (users.isPending) return <PageSpinner />;
  if (users.isError) return <ErrorNote error={users.error} />;
  return (
    <Card>
      <ul aria-label="Team members" className="divide-y divide-line">
        {users.data.map((u) => (
          <li key={u.id} className="flex items-center gap-3 px-4 py-3">
            <Avatar label={u.name || u.email} size={32} />
            <div className="min-w-0 flex-1">
              <div className="flex min-w-0 items-center gap-2">
                <span className="min-w-0 truncate font-medium" title={u.name || u.email}>
                  {u.name || u.email}
                </span>
                {me.data?.id === String(u.id) && <Badge>You</Badge>}
              </div>
              {u.name && (
                <div className="truncate text-xs text-muted" title={u.email}>
                  {u.email}
                </div>
              )}
            </div>
            <span className="hidden shrink-0 text-xs text-muted sm:inline">
              Joined <RelativeTime iso={u.created_at} />
            </span>
          </li>
        ))}
      </ul>
    </Card>
  );
}

function Invite({ onSent }: { onSent: (i: SentInvite) => void }) {
  const [email, setEmail] = useState("");
  const invite = useMutation({
    mutationFn: (to: string) => api.invites.create(to),
    onSuccess: ({ token }, to) => {
      onSent({ email: to, url: inviteURL(token) });
      setEmail("");
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (email.trim()) invite.mutate(email.trim());
  };
  return (
    <div className="space-y-2">
      <form onSubmit={submit} className="flex flex-col gap-2 sm:flex-row">
        <input type="email" required aria-label="Teammate's email" placeholder="teammate@company.com" value={email} onChange={(e) => setEmail(e.target.value)} className={inputClass} />
        <Button type="submit" variant="primary" disabled={invite.isPending}>
          Create invite link
        </Button>
      </form>
      <ErrorNote error={invite.error} />
    </div>
  );
}

function ChangePassword() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const change = useMutation({
    mutationFn: () => api.me.changePassword(current, next),
    onSuccess: () => {
      setCurrent("");
      setNext("");
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    change.mutate();
  };
  return (
    <form onSubmit={submit} className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="Current password">
          <input type="password" autoComplete="current-password" required value={current} onChange={(e) => setCurrent(e.target.value)} className={inputClass} />
        </Field>
        <Field label="New password">
          <input type="password" autoComplete="new-password" required value={next} onChange={(e) => setNext(e.target.value)} className={inputClass} />
        </Field>
      </div>
      <ErrorNote error={change.error} />
      <div className="flex items-center gap-3">
        <Button type="submit" disabled={change.isPending}>
          Change password
        </Button>
        {change.isSuccess && <span className="text-xs text-success">Password changed.</span>}
      </div>
    </form>
  );
}
