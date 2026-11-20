import { useState, type FormEvent, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useReauthMutation } from "./Reauth";
import { api, type ProviderStatus } from "../../api";
import { Markdown } from "../../components/Markdown";
import { keys, useProviders } from "../../queries";
import { Badge, Button, Card, ConfirmButton, CopyField, cx, ErrorNote, Icon, inputClass, labelClass, PageSpinner, RelativeTime } from "../../ui";
import { SettingsHeader, SettingsSubhead } from "./Settings";

export function Providers() {
  // Polls, so a delivery or a bad signature shows up while the user is wiring the webhook.
  const providers = useProviders({ poll: true });
  const all = providers.data ?? [];
  const connected = all.filter((p) => p.connected);
  const available = all.filter((p) => !p.connected);
  // One keyed list for both groups, so a card keeps its state (a secret shown once) when connecting moves it up.
  const items = [
    ...(connected.length > 0 ? [<SettingsSubhead key="connected">Connected</SettingsSubhead>] : []),
    ...connected.map((p) => <ProviderCard key={p.name} p={p} />),
    ...(available.length > 0
      ? [
          <SettingsSubhead key="available" className={cx(connected.length > 0 && "pt-6")}>
            {connected.length > 0 ? "Add another provider" : "Connect a provider"}
          </SettingsSubhead>,
        ]
      : []),
    ...available.map((p) => <ProviderCard key={p.name} p={p} />),
  ];
  return (
    <>
      <SettingsHeader title="Providers">
        Point your auth provider&apos;s webhook at Keyring and every sign-up becomes a contact. A connected provider shows its last delivery, so you can tell
        right away whether the setup worked.
      </SettingsHeader>
      {providers.isPending && <PageSpinner />}
      <ErrorNote error={providers.error} />
      <div className="space-y-3">{items}</div>
    </>
  );
}

type Health = { tone: "success" | "danger" | "info"; text: ReactNode };

function health(p: ProviderStatus): Health {
  const failedLast = p.last_error_at && (!p.last_delivery_at || Date.parse(p.last_error_at) > Date.parse(p.last_delivery_at));
  if (failedLast) {
    return {
      tone: "danger",
      text: (
        <>
          Last delivery failed <RelativeTime iso={p.last_error_at!} />: {p.last_error}
        </>
      ),
    };
  }
  if (p.last_delivery_at) {
    return {
      tone: "success",
      text: (
        <>
          Last delivery <RelativeTime iso={p.last_delivery_at} />
        </>
      ),
    };
  }
  return { tone: "info", text: "Connected. Waiting for the first webhook…" };
}

const healthStyles = {
  success: "text-success",
  danger: "text-danger bg-danger-soft px-2.5 py-1.5",
  info: "text-info",
};

function HealthLine({ h }: { h: Health }) {
  return (
    <p className={cx("flex items-start gap-1.5 rounded-control text-xs [overflow-wrap:anywhere]", healthStyles[h.tone])}>
      {h.tone === "info" && <span className="mt-1 size-1.5 shrink-0 animate-pulse rounded-full bg-current" />}
      {h.tone === "danger" && <Icon name="alert" className="mt-px size-3.5" />}
      <span className="min-w-0">{h.text}</span>
    </p>
  );
}

function ProviderCard({ p }: { p: ProviderStatus }) {
  const qc = useQueryClient();
  const [issued, setIssued] = useState<string | null>(null);
  const [secret, setSecret] = useState("");
  const [editing, setEditing] = useState(false);
  const [open, setOpen] = useState(false);
  const refresh = () => qc.invalidateQueries({ queryKey: keys.providers });

  const connect = useReauthMutation({
    mutationFn: (s: string) => api.providers.connect(p.name, s),
    onSuccess: (res) => {
      if (p.issue_secret) setIssued(res.secret);
      setSecret("");
      setEditing(false);
      return refresh();
    },
  });
  const disconnect = useReauthMutation({
    mutationFn: () => api.providers.disconnect(p.name),
    onSuccess: () => {
      setIssued(null);
      setOpen(false);
      return refresh();
    },
  });

  const webhookURL = location.origin + p.webhook_path;
  const setup = p.setup.replaceAll("{{webhook_url}}", webhookURL).replaceAll("{{secret}}", issued ?? "<your secret>");
  const expanded = p.connected || open || issued !== null;
  const bodyId = `provider-${p.name}`;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (secret.trim()) connect.mutate(secret.trim());
  };

  return (
    <Card>
      <div className="flex min-h-13 flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5">
        <h3 className="text-sm font-semibold">{p.title}</h3>
        {p.connected ? <Badge tone="success">Connected</Badge> : <span className="text-xs text-muted">Not connected</span>}
        <div className="ml-auto">
          {p.connected ? (
            <ConfirmButton label={`Disconnect ${p.title}`} confirmLabel="Disconnect?" onConfirm={() => disconnect.mutate()} disabled={disconnect.isPending}>
              Disconnect
            </ConfirmButton>
          ) : (
            <Button size="sm" variant={open ? "ghost" : "secondary"} aria-expanded={expanded} aria-controls={bodyId} onClick={() => setOpen(!open)}>
              {open ? "Close" : "Set up"}
              <Icon name="down" className={cx("size-3.5 transition-transform", open && "rotate-180")} />
            </Button>
          )}
        </div>
      </div>

      {expanded && (
        <div id={bodyId} className="space-y-4 border-t border-line p-4">
          {p.connected && <HealthLine h={health(p)} />}

          <div>
            <div className={labelClass}>Webhook URL</div>
            <CopyField value={webhookURL} />
          </div>

          {p.issue_secret ? (
            issued ? (
              <div className="rounded-surface border border-line-strong p-3">
                <div className="mb-2 text-sm">
                  <span className="font-medium">Signing secret.</span> <span className="text-muted">Copy it now. It is shown only once.</span>
                </div>
                <CopyField value={issued} />
              </div>
            ) : p.connected ? (
              <ConfirmButton label="Generate a new secret" confirmLabel="Replace the secret? The old one stops working." onConfirm={() => connect.mutate("")}>
                <Icon name="refresh" className="size-3.5" />
                Generate a new secret
              </ConfirmButton>
            ) : (
              <Button variant="primary" onClick={() => connect.mutate("")} disabled={connect.isPending}>
                <Icon name="key" className="size-3.5" />
                Generate signing secret
              </Button>
            )
          ) : p.connected && !editing ? (
            <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
              <Icon name="pencil" className="size-3.5" />
              Replace signing secret
            </Button>
          ) : (
            <form onSubmit={submit}>
              <label className={labelClass} htmlFor={`secret-${p.name}`}>
                Signing secret from {p.title}
              </label>
              <div className="flex flex-col gap-2 sm:flex-row">
                <input
                  id={`secret-${p.name}`}
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  placeholder="Paste the secret"
                  value={secret}
                  onChange={(e) => setSecret(e.target.value)}
                  className={cx(inputClass, "font-mono")}
                />
                <div className="flex gap-2">
                  <Button type="submit" variant="primary" disabled={!secret.trim() || connect.isPending}>
                    {p.connected ? "Save" : "Connect"}
                  </Button>
                  {editing && (
                    <Button variant="ghost" onClick={() => setEditing(false)}>
                      Cancel
                    </Button>
                  )}
                </div>
              </div>
            </form>
          )}
          <ErrorNote error={connect.error ?? disconnect.error} />

          {p.connected && issued === null && !editing ? (
            <details className="group border-t border-line pt-3">
              <summary className="flex cursor-pointer list-none items-center gap-1 text-xs font-medium text-muted select-none hover:text-fg">
                <Icon name="right" className="size-3.5 transition-transform group-open:rotate-90" />
                Setup instructions
              </summary>
              <div className="mt-3">
                <Markdown source={setup} />
              </div>
            </details>
          ) : (
            <div className="border-t border-line pt-4">
              <Markdown source={setup} />
            </div>
          )}
        </div>
      )}
    </Card>
  );
}
