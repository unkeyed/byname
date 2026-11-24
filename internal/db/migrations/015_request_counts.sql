-- Requests per caller address in fixed windows, for the unauthenticated
-- OAuth endpoints that write a row whether or not anyone approves it.
-- Unlike login_failures these are not failures: a refused caller waits for
-- the window to end, never longer.
CREATE TABLE request_counts (
    key          text PRIMARY KEY,
    window_start timestamptz NOT NULL,
    requests     int NOT NULL CHECK (requests > 0)
);
CREATE INDEX request_counts_window_start ON request_counts (window_start);
