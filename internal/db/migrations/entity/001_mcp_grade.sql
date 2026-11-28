ALTER TABLE instance_settings
    ADD COLUMN mcp_server_url text CHECK (length(mcp_server_url) BETWEEN 1 AND 2048),
    ADD COLUMN mcp_server_token text,
    ADD CONSTRAINT mcp_server_token_needs_url CHECK (mcp_server_token IS NULL OR mcp_server_url IS NOT NULL);

CREATE TABLE mcp_grade (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    graded_at timestamptz NOT NULL DEFAULT now(),
    url text NOT NULL,
    score smallint NOT NULL CHECK (score BETWEEN 0 AND 100),
    grade text NOT NULL CHECK (grade IN ('A+', 'A', 'B', 'C', 'D', 'F')),
    rules text NOT NULL,
    report jsonb NOT NULL
);
