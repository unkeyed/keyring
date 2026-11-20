import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import { api, type ConsentRequest } from "../api";
import { keys, useMe } from "../queries";
import { Badge, Button, ErrorNote, Icon, PageSpinner } from "../ui";
import { ReauthProvider, useReauthMutation } from "./settings/Reauth";
import { AuthShell } from "./Login";

/** Why /oauth/authorize refused. uncrm explains here rather than redirecting, since nobody has approved the app's address yet. */
const refusals: Record<string, string> = {
  invalid_client: "This app is not registered with uncrm, so uncrm cannot tell who is asking. Start connecting again from the app.",
  invalid_redirect_uri: "The app asked uncrm to send you to an address it never registered, so uncrm stopped here instead.",
  invalid_request: "The app did not protect its sign-in with PKCE, which uncrm requires. Update the app, or ask its maker to support PKCE.",
  unsupported_response_type: "The app asked for a kind of sign-in uncrm does not offer. uncrm supports only the authorization code flow.",
  invalid_target: "The app asked for access to something other than this uncrm's MCP server. Check the server URL in the app's settings.",
};

/**
 * The OAuth consent page. /oauth/authorize sends a person here with the
 * pending request; signing in comes first, through the usual 401 to /login.
 */
export function Consent() {
  const [params] = useSearchParams();
  const handle = params.get("request");
  if (!handle) {
    const reason = refusals[params.get("error") ?? ""] ?? "This link does not hold a sign-in request. Start connecting again from the app.";
    return (
      <AuthShell title="Can't connect this app">
        <p className="text-sm text-muted">{reason}</p>
      </AuthShell>
    );
  }
  return (
    <ReauthProvider>
      <Pending handle={handle} />
    </ReauthProvider>
  );
}

function Pending({ handle }: { handle: string }) {
  const req = useQuery({ queryKey: keys.consent(handle), queryFn: () => api.consent.get(handle), staleTime: Infinity, retry: false });
  const me = useMe();
  if (req.isPending || me.isPending) {
    return (
      <AuthShell title="Connect an app">
        <PageSpinner />
      </AuthShell>
    );
  }
  if (req.isError) {
    return (
      <AuthShell title="Can't connect this app">
        <ErrorNote error={req.error} />
      </AuthShell>
    );
  }
  return <Decide handle={handle} req={req.data} me={me.data?.label ?? "you"} />;
}

/** Where the browser goes after approval, said in words a person can check against what they just did. */
function destination(uri: string): { where: string; what: string } {
  const u = new URL(uri);
  if (u.protocol === "https:") return { where: u.host, what: "a website" };
  if (u.protocol === "http:") return { where: u.host, what: "an app on this computer" };
  return { where: `${u.protocol}//${u.host}`, what: "an app on this device" };
}

function Decide({ handle, req, me }: { handle: string; req: ConsentRequest; me: string }) {
  const [sent, setSent] = useState<boolean | null>(null);
  const decide = useReauthMutation({
    mutationFn: (approve: boolean) => api.consent.decide(handle, approve),
    onSuccess: ({ redirect }, approve) => {
      setSent(approve);
      window.location.assign(redirect);
    },
  });
  const to = destination(req.redirect_uri);

  if (sent !== null) {
    return (
      <AuthShell title={sent ? "Connected" : "Declined"}>
        <p className="text-sm text-muted">
          {sent ? <>Your browser is handing the sign-in back to {to.what}. </> : <>The app was told no. </>}
          You can close this tab.
        </p>
      </AuthShell>
    );
  }

  return (
    <AuthShell title="Connect an app to uncrm">
      <div className="space-y-4">
        <div className="flex items-start gap-3">
          <span className="grid size-10 shrink-0 place-items-center rounded-control bg-subtle text-muted">
            <Icon name="bot" className="size-5" />
          </span>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <span className="text-base font-semibold [overflow-wrap:anywhere]">{req.client_name}</span>
              <Badge tone="warn" title="Any app can register under any name. uncrm cannot check this one.">
                Unverified
              </Badge>
            </div>
            <p className="mt-0.5 text-sm text-muted">
              wants to use your CRM as <span className="font-medium text-fg">{me}</span>.
            </p>
          </div>
        </div>

        <div className="rounded-control border border-line bg-subtle p-3 text-sm">
          <p className="text-xs font-medium text-muted">After you approve, your browser goes to</p>
          <p className="mt-1 font-mono text-sm font-semibold [overflow-wrap:anywhere]">{to.where}</p>
          <p className="mt-0.5 text-xs text-muted">
            {to.what} · <span className="font-mono [overflow-wrap:anywhere]">{req.redirect_uri}</span>
          </p>
        </div>

        <ul className="space-y-1.5 text-sm text-muted">
          <li className="flex gap-2">
            <Icon name="check" className="mt-0.5 size-3.5 shrink-0 text-fg" />
            Search, read and edit contacts and companies, add notes and set stages.
          </li>
          <li className="flex gap-2">
            <Icon name="check" className="mt-0.5 size-3.5 shrink-0 text-fg" />
            Its changes show on the timeline as {req.client_name} (via {me}).
          </li>
          <li className="flex gap-2">
            <Icon name="x" className="mt-0.5 size-3.5 shrink-0" />
            It cannot manage keys, providers or your team.
          </li>
        </ul>

        <p className="text-xs text-muted">
          Approve only if you started connecting this app just now. You can disconnect it any time under Settings, Agents &amp; MCP.
        </p>

        <ErrorNote error={decide.error} />
        <div className="flex gap-2">
          <Button className="flex-1" onClick={() => decide.mutate(false)} disabled={decide.isPending}>
            Deny
          </Button>
          <Button variant="primary" className="flex-1" onClick={() => decide.mutate(true)} disabled={decide.isPending}>
            {decide.isPending ? "Connecting…" : "Approve"}
          </Button>
        </div>
      </div>
    </AuthShell>
  );
}
