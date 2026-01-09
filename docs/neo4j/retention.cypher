// Retention cleanup for logs and telemetry (30 days).
// Assumes timestamps are epoch milliseconds.

WITH timestamp() - 30 * 24 * 60 * 60 * 1000 AS cutoff
MATCH (t:TelemetrySample)
WHERE t.ts < cutoff
DETACH DELETE t;

WITH timestamp() - 30 * 24 * 60 * 60 * 1000 AS cutoff
MATCH (cmd:ActionCommand)
WHERE cmd.created_at < cutoff
DETACH DELETE cmd;

WITH timestamp() - 30 * 24 * 60 * 60 * 1000 AS cutoff
MATCH (res:ActionResult)
WHERE res.completed_at < cutoff
DETACH DELETE res;

WITH timestamp() - 30 * 24 * 60 * 60 * 1000 AS cutoff
MATCH (se:StepExecution)
WHERE se.completed_at < cutoff
DETACH DELETE se;

WITH timestamp() - 30 * 24 * 60 * 60 * 1000 AS cutoff
MATCH (e:Execution)
WHERE e.updated_at < cutoff
DETACH DELETE e;
