import { useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router";
import { api, ApiError } from "../api";
import { keys, useStages } from "../queries";
import { useToast } from "./Toaster";
import { Button, Dialog, DialogBody, DialogFooter, ErrorNote, Field, inputClass, selectClass } from "../ui";

/** A 409 from a create names the record that already exists; link to it instead of showing the raw error. */
function CreateError({ error, to, onNavigate }: { error: unknown; to: string; onNavigate: () => void }) {
  if (error instanceof ApiError && error.status === 409 && error.existingId) {
    return (
      <p role="alert" className="rounded-control bg-subtle px-2.5 py-1.5 text-xs text-muted">
        Already in Keyring.{" "}
        <Link to={`${to}/${error.existingId}`} onClick={onNavigate} className="font-medium text-fg underline underline-offset-2">
          Open it
        </Link>
      </p>
    );
  }
  return <ErrorNote error={error} />;
}

function CreateForm({
  onSubmit,
  onCancel,
  busy,
  submit,
  error,
  children,
}: {
  onSubmit: () => void;
  onCancel: () => void;
  busy: boolean;
  submit: string;
  error: ReactNode;
  children: ReactNode;
}) {
  const handle = (e: FormEvent) => {
    e.preventDefault();
    onSubmit();
  };
  return (
    <form onSubmit={handle}>
      <DialogBody>
        <div className="space-y-3">
          {children}
          {error}
        </div>
      </DialogBody>
      <DialogFooter>
        <Button variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" variant="primary" disabled={busy}>
          {busy ? "Adding…" : submit}
        </Button>
      </DialogFooter>
    </form>
  );
}

export function NewContactDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog open={open} onClose={onClose} title="New contact">
      <NewContactForm onClose={onClose} />
    </Dialog>
  );
}

/** Dialog remounts it on every opening, so each one starts empty. */
function NewContactForm({ onClose }: { onClose: () => void }) {
  const stages = useStages();
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [title, setTitle] = useState("");
  const [stage, setStage] = useState("");
  const qc = useQueryClient();
  const navigate = useNavigate();
  const toast = useToast();
  const create = useMutation({
    mutationFn: () => api.contacts.create({ email: email.trim(), name: name.trim(), title: title.trim(), stage: stage || undefined }),
    onSuccess: (c) => {
      void qc.invalidateQueries({ queryKey: keys.contacts });
      void qc.invalidateQueries({ queryKey: keys.status });
      onClose();
      toast(`Added ${c.name || c.email}`);
      void navigate(`/contacts/${c.id}`, { viewTransition: true });
    },
  });
  return (
    <CreateForm
      onSubmit={() => create.mutate()}
      onCancel={onClose}
      busy={create.isPending}
      submit="Add contact"
      error={<CreateError error={create.error} to="/contacts" onNavigate={onClose} />}
    >
      <Field label="Email" hint="Their company is found from the email domain, then enriched.">
        <input type="email" required autoFocus data-autofocus autoComplete="off" value={email} onChange={(e) => setEmail(e.target.value)} className={inputClass} />
      </Field>
      <Field label="Name">
        <input autoComplete="off" value={name} onChange={(e) => setName(e.target.value)} className={inputClass} />
      </Field>
      <Field label="Title">
        <input autoComplete="off" value={title} onChange={(e) => setTitle(e.target.value)} className={inputClass} />
      </Field>
      <Field label="Stage">
        <select value={stage || stages.data?.[0] || ""} onChange={(e) => setStage(e.target.value)} className={`${selectClass} w-full`}>
          {stages.data?.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </Field>
    </CreateForm>
  );
}

export function NewCompanyDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog open={open} onClose={onClose} title="New company">
      <NewCompanyForm onClose={onClose} />
    </Dialog>
  );
}

/** "https://www.stripe.com/pricing" is stripe.com; the server takes a bare domain. */
const bareDomain = (s: string) =>
  s
    .trim()
    .replace(/^[a-z]+:\/\//i, "")
    .replace(/[/?#].*$/, "")
    .replace(/^www\./i, "");

function NewCompanyForm({ onClose }: { onClose: () => void }) {
  const [domain, setDomain] = useState("");
  const [name, setName] = useState("");
  const qc = useQueryClient();
  const navigate = useNavigate();
  const toast = useToast();
  const create = useMutation({
    mutationFn: () => api.companies.create({ domain: bareDomain(domain), name: name.trim() }),
    onSuccess: (c) => {
      void qc.invalidateQueries({ queryKey: keys.companies });
      void qc.invalidateQueries({ queryKey: keys.status });
      onClose();
      toast(`Added ${c.name || c.domain}`);
      void navigate(`/companies/${c.id}`, { viewTransition: true });
    },
  });
  return (
    <CreateForm
      onSubmit={() => create.mutate()}
      onCancel={onClose}
      busy={create.isPending}
      submit="Add company"
      error={<CreateError error={create.error} to="/companies" onNavigate={onClose} />}
    >
      <Field label="Domain" hint="Like acme.com. The logo, links and details are filled in from it.">
        <input required autoFocus data-autofocus autoComplete="off" spellCheck={false} placeholder="acme.com" value={domain} onChange={(e) => setDomain(e.target.value)} className={inputClass} />
      </Field>
      <Field label="Name" hint="Optional. Leave it empty to use the name enrichment finds.">
        <input autoComplete="off" value={name} onChange={(e) => setName(e.target.value)} className={inputClass} />
      </Field>
    </CreateForm>
  );
}
