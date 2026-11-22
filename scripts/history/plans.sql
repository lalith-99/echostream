-- Pull the actual cursor query issued by the benchmark from pg_stat_statements.
-- Its parameters differ between the four-command and single-statement paths.
SELECT calls, rows, round((total_exec_time/calls)::numeric,4) AS mean_exec_ms,
       round((shared_blks_hit::numeric+shared_blks_read)/calls,2) AS buffers_per_call
FROM pg_stat_statements
WHERE query ILIKE '%FROM messages%' AND query ILIKE '%ORDER BY id ASC%'
ORDER BY calls DESC LIMIT 1;
-- Restore the normalized ON true constant; it is not a bound application argument.
SELECT 'PREPARE history_plan AS ' || regexp_replace(query, 'ON \$[0-9]+ ORDER BY p.id', 'ON true ORDER BY p.id', 'i')
FROM pg_stat_statements
WHERE query ILIKE '%FROM messages%' AND query ILIKE '%ORDER BY id ASC%'
  AND query NOT ILIKE '%pg_stat_statements%'
ORDER BY calls DESC LIMIT 1
\gexec
SELECT format('EXPLAIN (ANALYZE, BUFFERS, COSTS OFF) EXECUTE history_plan(%s)',
  CASE cardinality(parameter_types)
    WHEN 4 THEN '''10000000-0000-0000-0000-000000000001'',''30000000-0000-0000-0000-000000000001'',20,500001'
    ELSE '''10000000-0000-0000-0000-000000000001'',''30000000-0000-0000-0000-000000000001'',''20000000-0000-0000-0000-000000000001'',20,500001'
  END)
FROM pg_prepared_statements WHERE name='history_plan'
\gexec
