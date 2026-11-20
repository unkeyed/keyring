import { Link } from "react-router";
import { useProviders } from "../queries";
import { buttonClass, CodeBlock, cx, Icon } from "../ui";

const or = new Intl.ListFormat("en", { type: "disjunction" });

export function signupCurl(origin: string): string {
  return `curl -X POST ${origin}/v1/signups \\
  -H "Authorization: Bearer $KEYRING_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"email": "ada@example.com", "name": "Ada Lovelace"}'`;
}

/** The contacts list before the first sign-up. */
export function Onboarding() {
  const providers = useProviders();
  const names = providers.data?.map((p) => p.title) ?? [];
  return (
    <div className="mx-auto max-w-2xl px-4 py-10 md:px-5 md:py-14">
      <div className="flex items-center gap-2 text-xs font-medium text-muted">
        <span className="relative flex size-2">
          <span className="absolute inline-flex size-full animate-ping rounded-full bg-success opacity-60" />
          <span className="relative inline-flex size-2 rounded-full bg-success" />
        </span>
        Listening for sign-ups
      </div>
      <h2 className="mt-3 text-xl font-semibold">Bring in your first sign-up</h2>
      <p className="mt-1.5 text-sm text-muted">
        Keyring records everyone who signs up to your product. Pick one way in. New contacts appear here on their own, no reload needed.
      </p>

      <ol className="mt-8 space-y-4">
        <li className="rounded-surface border border-line bg-surface p-4">
          <div className="flex items-start gap-3">
            <span className="grid size-6 shrink-0 place-items-center rounded-full bg-subtle text-xs font-semibold text-fg">1</span>
            <div className="min-w-0 flex-1">
              <h3 className="font-medium">Connect your auth provider</h3>
              <p className="mt-0.5 text-sm text-muted">
                {names.length > 0 && `${or.format(names)}. `}Paste one webhook URL and every new sign-up flows in.
              </p>
              <Link
                to="/settings/providers"
                className={cx(buttonClass({ variant: "primary" }), "mt-3")}
              >
                <Icon name="plug" className="size-3.5" />
                Connect a provider
              </Link>
            </div>
          </div>
        </li>
        <li className="rounded-surface border border-line bg-surface p-4">
          <div className="flex items-start gap-3">
            <span className="grid size-6 shrink-0 place-items-center rounded-full bg-subtle text-xs font-semibold text-muted">2</span>
            <div className="min-w-0 flex-1">
              <h3 className="font-medium">Or send sign-ups from your own backend</h3>
              <p className="mt-0.5 mb-3 text-sm text-muted">
                Any auth system works. Call this when someone signs up, with a key from{" "}
                <Link to="/settings/keys" className="text-fg underline underline-offset-2 hover:opacity-70">
                  Settings, API keys
                </Link>
                .
              </p>
              <CodeBlock code={signupCurl(location.origin)} />
            </div>
          </div>
        </li>
      </ol>
    </div>
  );
}
