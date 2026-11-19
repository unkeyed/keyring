# Clerk

Clerk sends webhooks through Svix, signed with the Standard Webhooks scheme. Setup is one endpoint and one pasted secret.

1. In uncrm, open **Settings > Providers > Clerk** and copy the webhook URL (`https://<your-uncrm-host>/webhooks/clerk`).
2. In the [Clerk Dashboard](https://dashboard.clerk.com), pick your application and instance (Development or Production), open **Webhooks**, and click **Add Endpoint**.
3. Set **Endpoint URL** to the URL from step 1.
4. Under **Subscribe to events**, select `user.created`, `user.updated` and `user.deleted`, then click **Create**.
5. On the endpoint's page, reveal the **Signing Secret** (it starts with `whsec_`), copy it, and paste it into uncrm.
6. Use the endpoint's **Testing** tab to send a `user.created` example. uncrm's Settings page shows the delivery within a few seconds.

Development and Production instances have separate webhooks. Repeat these steps for Production when you go live.

## What uncrm reads

| uncrm field | Clerk field |
|---|---|
| external id | `data.id` |
| email | the entry in `data.email_addresses` whose `id` is `data.primary_email_address_id` |
| email verified | that entry's `verification.status` is `verified` |
| name | `data.first_name` + `last_name`, else `data.username` |
| avatar | `data.image_url` |
| occurred at | the event's `timestamp` |

Other events, such as `session.created`, and users without an email (phone or web3 only) are accepted with a 200 and ignored.

## Verification

uncrm checks the `svix-signature` header, an HMAC-SHA256 of `svix-id.svix-timestamp.body` keyed by the secret, and rejects deliveries more than 5 minutes old.
