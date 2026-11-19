import { useState, type FormEvent } from "react";
import { useMutation } from "@tanstack/react-query";
import { api, inviteURL } from "../../api";
import { useMe } from "../../queries";
import { Avatar, Button, Card, CopyField, ErrorNote, Field, inputClass } from "../../ui";
import { SettingsHeader } from "./Settings";

export function Team() {
  const me = useMe();
  return (
    <>
      <SettingsHeader title="Team">Invite teammates with a one-time link. Everyone on the team can see and edit every contact.</SettingsHeader>
      <Invite />

      <h3 className="mt-10 mb-3 text-[13px] font-semibold">Your account</h3>
      <Card className="p-4">
        {me.data && (
          <div className="mb-5 flex items-center gap-3">
            <Avatar label={me.data.label} size={32} />
            <div className="text-[13px] font-medium">{me.data.label}</div>
          </div>
        )}
        <ChangePassword />
      </Card>
    </>
  );
}

function Invite() {
  const [email, setEmail] = useState("");
  const [link, setLink] = useState<{ email: string; url: string } | null>(null);
  const invite = useMutation({
    mutationFn: () => api.invites.create(email.trim()),
    onSuccess: ({ token }) => {
      setLink({ email: email.trim(), url: inviteURL(token) });
      setEmail("");
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (email.trim()) invite.mutate();
  };
  return (
    <div className="space-y-3">
      <form onSubmit={submit} className="flex gap-2">
        <input type="email" required aria-label="Teammate's email" placeholder="teammate@company.com" value={email} onChange={(e) => setEmail(e.target.value)} className={inputClass} />
        <Button type="submit" variant="primary" disabled={invite.isPending} className="shrink-0">
          Create invite link
        </Button>
      </form>
      <ErrorNote error={invite.error} />
      {link && (
        <div className="rounded-md border border-line bg-subtle p-3">
          <div className="mb-1.5 text-xs text-muted">
            Send this link to <b className="text-fg">{link.email}</b>. It works once and expires in 7 days.
          </div>
          <CopyField value={link.url} />
        </div>
      )}
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
      <div className="grid grid-cols-2 gap-3">
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
