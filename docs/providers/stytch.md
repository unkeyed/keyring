# Stytch

Stytch sends webhooks through Svix, signed with the Standard Webhooks scheme. Setup is one endpoint and one pasted secret. Both Consumer and B2B projects work.

1. In uncrm, open **Settings > Providers > Stytch** and copy the webhook URL (`https://<your-uncrm-host>/webhooks/stytch`).
2. In the [Stytch Dashboard](https://stytch.com/dashboard), pick the project and environment (Test or Live) your app uses, open **Webhooks**, and click **Add Endpoint**.
3. Set **Endpoint URL** to the URL from step 1.
4. Under **Subscribe to events**, select:
   - Consumer projects: `direct.user.create`, `direct.user.update`, `direct.user.delete`, `dashboard.user.create`, `dashboard.user.update` and `dashboard.user.delete`.
   - B2B projects: the `create`, `update` and `delete` events for `direct.member`, `dashboard.member` and `scim.member`.
5. Click **Create**. On the endpoint's page, reveal the **Signing Secret** (it starts with `whsec_`), copy it, and paste it into uncrm.
6. Use the endpoint's **Testing** tab to send a `direct.user.create` example. uncrm's Settings page shows the delivery within a few seconds.

Test and Live are separate environments with separate webhooks. Repeat these steps for Live when you go live.

## What uncrm reads

| uncrm field | Consumer (`object_type: user`) | B2B (`object_type: member`) |
|---|---|---|
| external id | top-level `id` (the `user_id`) | top-level `id` (the `member_id`) |
| email | `user.emails[0].email` | `member.email_address` |
| email verified | `user.emails[0].verified` | `member.email_address_verified` |
| name | `user.name.first_name` + `last_name` | `member.name` |
| avatar | the first `user.providers[].profile_picture_url` | none |
| occurred at | `timestamp` | `timestamp` |

`action` is `CREATE`, `UPDATE` or `DELETE`. A delete carries only the id. Other object types (such as `organization`) and users without an email are accepted with a 200 and ignored.

## Verification

uncrm checks the `svix-signature` header (or `webhook-signature`), an HMAC-SHA256 of `id.timestamp.body` keyed by the secret, and rejects deliveries more than 5 minutes old.
