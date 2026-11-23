-- Which process is executing a run, and until when its claim holds. The
-- process renews lease_until while it works, so one that hangs or is
-- replaced stops renewing, and another takes the run over once it passes.
-- The claim does not depend on a connection staying open.
ALTER TABLE run
    ADD COLUMN lease_owner text,
    ADD COLUMN lease_until timestamptz;
