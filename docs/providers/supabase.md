# Supabase Auth

Supabase Auth has no sign-up webhook, so uncrm gives you a SQL snippet that adds triggers on `auth.users`. The triggers use [pg_net](https://supabase.com/docs/guides/database/extensions/pg_net) to POST each new, changed or deleted user to uncrm with `Authorization: Bearer <secret>`. They cover every sign-up method: email, magic link, OAuth, SSO, phone and admin-created users.

## Setup

1. In uncrm, open **Settings > Providers > Supabase Auth** and click **Connect**. uncrm shows the SQL below with your webhook URL and secret already filled in. Copy it from there.
2. In the [Supabase Dashboard](https://supabase.com/dashboard), open your project's **SQL Editor** and click **New query**.
3. Paste the SQL and click **Run**.
4. Sign up a test user. uncrm's Settings page shows the delivery within a few seconds.

If you copy the SQL from this page instead, replace `{{webhook_url}}` (`https://<your-uncrm-host>/webhooks/supabase`) and both copies of `{{secret}}`.

```sql
create extension if not exists pg_net with schema extensions;

-- The secret lives in Vault so it is not written into the function body.
do $$
begin
  perform vault.update_secret(id, '{{secret}}') from vault.secrets where name = 'uncrm_webhook_secret';
  if not found then
    perform vault.create_secret('{{secret}}', 'uncrm_webhook_secret');
  end if;
end $$;

create schema if not exists uncrm;
revoke all on schema uncrm from public, anon, authenticated;

-- Sends one auth.users row to uncrm. Only the columns listed here leave the
-- database: never to_jsonb(u), which would send the password hash and tokens.
create or replace function uncrm.send_user(op text, u auth.users)
returns void
language plpgsql
security definer
set search_path = ''
as $$
begin
  perform net.http_post(
    url := '{{webhook_url}}',
    headers := jsonb_build_object(
      'Content-Type', 'application/json',
      'Authorization', 'Bearer ' || (select decrypted_secret from vault.decrypted_secrets where name = 'uncrm_webhook_secret')
    ),
    body := jsonb_build_object(
      'type', op,
      'timestamp', case when op = 'INSERT' then u.created_at else now() end,
      'record', jsonb_build_object(
        'id', u.id,
        'email', u.email,
        'email_confirmed_at', u.email_confirmed_at,
        'is_anonymous', u.is_anonymous,
        'deleted_at', u.deleted_at,
        'created_at', u.created_at,
        'raw_user_meta_data', jsonb_build_object(
          'full_name', u.raw_user_meta_data ->> 'full_name',
          'name', u.raw_user_meta_data ->> 'name',
          'avatar_url', u.raw_user_meta_data ->> 'avatar_url',
          'picture', u.raw_user_meta_data ->> 'picture'
        )
      )
    ),
    timeout_milliseconds := 5000
  );
exception when others then
  -- Never block a sign-up because uncrm is unreachable or misconfigured.
  raise warning 'uncrm: %', sqlerrm;
end;
$$;

create or replace function uncrm.on_user_change()
returns trigger
language plpgsql
security definer
set search_path = ''
as $$
begin
  if tg_op = 'DELETE' then
    perform uncrm.send_user(tg_op, old);
    return old;
  end if;
  perform uncrm.send_user(tg_op, new);
  return new;
end;
$$;

revoke all on all functions in schema uncrm from public, anon, authenticated;

drop trigger if exists uncrm_user_insert_delete on auth.users;
create trigger uncrm_user_insert_delete
  after insert or delete on auth.users
  for each row execute function uncrm.on_user_change();

-- Sign-ins also update auth.users; only send the changes uncrm stores.
drop trigger if exists uncrm_user_update on auth.users;
create trigger uncrm_user_update
  after update on auth.users
  for each row
  when (
    old.email is distinct from new.email
    or old.email_confirmed_at is distinct from new.email_confirmed_at
    or old.raw_user_meta_data is distinct from new.raw_user_meta_data
    or old.deleted_at is distinct from new.deleted_at
  )
  execute function uncrm.on_user_change();
```

Running it again is safe. It updates the secret in Vault and replaces the functions and triggers, so it is also how you rotate the secret.

## Send the users you already have

The triggers only see changes made after you install them. To send every existing user once, run:

```sql
select uncrm.send_user('INSERT', u) from auth.users u where u.email is not null and not u.is_anonymous;
```

Each one arrives as a sign-up dated with the user's `created_at`. Running it twice is harmless, because uncrm dedupes sign-ups per user.

## What leaves your database

Only these columns: `id`, `email`, `email_confirmed_at`, `is_anonymous`, `deleted_at`, `created_at`, and the `full_name`, `name`, `avatar_url` and `picture` keys of `raw_user_meta_data`.

The SQL builds the body from that list on purpose. Supabase's generic Database Webhooks send `to_jsonb(new)`, which for `auth.users` includes `encrypted_password` (the password hash) and the confirmation and recovery tokens. Don't point a Database Webhook at uncrm.

## What uncrm reads

| uncrm field | Supabase field |
|---|---|
| external id | `record.id` |
| email | `record.email` |
| email verified | `record.email_confirmed_at` is set |
| name | `raw_user_meta_data.full_name`, else `.name` |
| avatar | `raw_user_meta_data.avatar_url`, else `.picture` |
| occurred at | `created_at` for a new user, the time of the change otherwise |

Anonymous users and users without an email (phone only) are accepted with a 200 and ignored. An update that sets `deleted_at` (a soft delete) counts as a delete. Sign-ins also update `auth.users`, but the update trigger only fires when the email, its confirmation, the user metadata or `deleted_at` changes.

## Failure behaviour

pg_net sends asynchronously after the transaction commits, so a slow or unreachable uncrm never slows down or blocks a sign-up. Any error inside the trigger is downgraded to a Postgres warning for the same reason.

pg_net does not retry. A delivery that fails while uncrm is down is lost. Re-run the backfill query above to catch up, or look at `net._http_response` (kept for 6 hours) to see what failed.

## Security notes

- The secret is stored in [Supabase Vault](https://supabase.com/docs/guides/database/vault), not in the function body. pg_net still keeps request headers in `net.http_request_queue` until the request is sent, so any role that can log in to your database directly can read the secret during that window. The secret only lets someone post fake sign-ups to uncrm.
- The functions live in their own `uncrm` schema, with no grants to `anon` or `authenticated`, so they are not reachable through the Supabase API.

## Uninstall

```sql
drop trigger if exists uncrm_user_insert_delete on auth.users;
drop trigger if exists uncrm_user_update on auth.users;
drop schema if exists uncrm cascade;
delete from vault.secrets where name = 'uncrm_webhook_secret';
```
