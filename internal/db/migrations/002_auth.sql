-- Failed logins per email, shared by every replica, so login throttling
-- holds however many instances run. Rows exist for unknown emails too, so
-- throttling never reveals whether an account exists. A success deletes
-- the row; rows idle for a day are swept on the next failure.
-- Written only by internal/auth (Users.Login).
CREATE TABLE login_failures (
    email          text PRIMARY KEY CHECK (email = lower(btrim(email))),
    failures       int NOT NULL CHECK (failures > 0),
    last_failed_at timestamptz NOT NULL
);
CREATE INDEX login_failures_last_failed ON login_failures (last_failed_at);
