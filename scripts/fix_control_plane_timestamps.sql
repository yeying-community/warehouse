\set ON_ERROR_STOP on

\if :{?source_tz}
\else
\set source_tz 'Asia/Shanghai'
\endif

-- Usage:
--   psql "$DATABASE_URL" -v source_tz='Asia/Shanghai' -f scripts/fix_control_plane_timestamps.sql
--
-- Purpose:
--   Fix historical control-plane rows written before UTC normalization for:
--   - cluster_nodes.created_at / updated_at
--   - cluster_replication_assignments.created_at / updated_at
--
-- Notes:
--   1. The script only updates rows with an obvious timezone skew.
--   2. New UTC-normalized rows will not match the WHERE clauses.
--   3. Active assignment lease_expires_at and node last_heartbeat_at are treated as the UTC baseline.

BEGIN;

\echo ''
\echo '== Preview: cluster_nodes rows with obvious local-time residue =='
SELECT
  node_id,
  role,
  last_heartbeat_at,
  created_at,
  updated_at
FROM cluster_nodes
WHERE created_at > last_heartbeat_at + INTERVAL '4 hours'
   OR updated_at > last_heartbeat_at + INTERVAL '4 hours'
ORDER BY node_id;

\echo ''
\echo '== Preview: cluster_replication_assignments rows with obvious local-time residue =='
SELECT
  id,
  active_node_id,
  standby_node_id,
  state,
  generation,
  lease_expires_at,
  created_at,
  updated_at
FROM cluster_replication_assignments
WHERE lease_expires_at IS NOT NULL
  AND (
    created_at > lease_expires_at + INTERVAL '4 hours'
    OR updated_at > lease_expires_at + INTERVAL '4 hours'
  )
ORDER BY id;

\echo ''
\echo '== Fix: cluster_nodes.created_at =='
UPDATE cluster_nodes
SET created_at = ((created_at AT TIME ZONE :'source_tz') AT TIME ZONE 'UTC')
WHERE created_at > last_heartbeat_at + INTERVAL '4 hours';

\echo ''
\echo '== Fix: cluster_nodes.updated_at =='
UPDATE cluster_nodes
SET updated_at = ((updated_at AT TIME ZONE :'source_tz') AT TIME ZONE 'UTC')
WHERE updated_at > last_heartbeat_at + INTERVAL '4 hours';

\echo ''
\echo '== Fix: cluster_replication_assignments.created_at =='
UPDATE cluster_replication_assignments
SET created_at = ((created_at AT TIME ZONE :'source_tz') AT TIME ZONE 'UTC')
WHERE lease_expires_at IS NOT NULL
  AND created_at > lease_expires_at + INTERVAL '4 hours';

\echo ''
\echo '== Fix: cluster_replication_assignments.updated_at =='
UPDATE cluster_replication_assignments
SET updated_at = ((updated_at AT TIME ZONE :'source_tz') AT TIME ZONE 'UTC')
WHERE lease_expires_at IS NOT NULL
  AND updated_at > lease_expires_at + INTERVAL '4 hours';

\echo ''
\echo '== Result: cluster_nodes =='
SELECT
  node_id,
  role,
  last_heartbeat_at,
  created_at,
  updated_at
FROM cluster_nodes
ORDER BY node_id;

\echo ''
\echo '== Result: cluster_replication_assignments =='
SELECT
  id,
  active_node_id,
  standby_node_id,
  state,
  generation,
  lease_expires_at,
  created_at,
  updated_at
FROM cluster_replication_assignments
ORDER BY id;

COMMIT;
