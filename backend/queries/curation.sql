-- name: CreateCurationRun :one
INSERT INTO curation_runs (kind, rule, started_by, automatic, dry_run, state, bulk_change_id, finished_at)
VALUES (sqlc.arg(kind), sqlc.narg(rule), sqlc.narg(started_by), sqlc.arg(automatic), sqlc.arg(dry_run), sqlc.arg(state), sqlc.narg(bulk_change_id),
        CASE WHEN sqlc.arg(state) = 'done' THEN now() END)
RETURNING id;

-- name: GetCurationRunOfBulkChange :one
SELECT id, automatic FROM curation_runs WHERE bulk_change_id = $1;

-- name: GetCurationRunForUpdate :one
SELECT id, automatic, state, dry_run FROM curation_runs WHERE id = $1 FOR UPDATE;

-- name: FinishCurationRun :exec
UPDATE curation_runs SET state = 'done', finished_at = now() WHERE bulk_change_id = $1 AND state = 'running';

-- name: InsertCurationAction :one
INSERT INTO curation_actions (run_id, library_id, kind, rule, reason, books, trashed_files, before, after)
VALUES (sqlc.arg(run_id), sqlc.arg(library_id), sqlc.arg(kind), sqlc.narg(rule), sqlc.arg(reason),
        sqlc.arg(books)::uuid[], sqlc.arg(trashed_files)::uuid[], sqlc.arg(before), sqlc.arg(after))
RETURNING id;

-- name: ListCurationRuns :many
-- The runs with an action in a library the viewer sees, or a run of the
-- viewer's own that has none yet, the newest first. Counts are of the
-- actions the viewer sees.
SELECT r.id, r.kind, r.rule, r.started_by, u.username AS started_by_name, r.dry_run, r.automatic, r.state,
       r.created_at, r.finished_at, r.confirmed_at, r.undone_at, r.skipped, (r.undo_requested_at IS NOT NULL)::boolean AS undoing,
       (SELECT count(*) FROM curation_actions a
         WHERE a.run_id = r.id AND a.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND books_age_visible(a.books, sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))::int AS actions,
       (SELECT count(*) FROM curation_actions a
         WHERE a.run_id = r.id AND a.state = 'undone'
           AND a.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND books_age_visible(a.books, sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))::int AS undone
FROM curation_runs r
LEFT JOIN users u ON u.id = r.started_by
WHERE (EXISTS (SELECT 1 FROM curation_actions a
               WHERE a.run_id = r.id AND a.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND books_age_visible(a.books, sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
       OR (r.started_by = sqlc.arg(viewer)::uuid AND NOT EXISTS (SELECT 1 FROM curation_actions a WHERE a.run_id = r.id)))
  AND (sqlc.narg(kind)::text IS NULL OR r.kind = sqlc.narg(kind)::text)
  AND (sqlc.narg(state)::text IS NULL OR r.state = sqlc.narg(state)::text)
  AND (sqlc.narg(after_at)::timestamptz IS NULL OR (r.created_at, r.id) < (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY r.created_at DESC, r.id DESC
LIMIT sqlc.arg(max_rows);

-- name: GetCurationRun :one
SELECT r.id, r.kind, r.rule, r.started_by, u.username AS started_by_name, r.dry_run, r.automatic, r.state,
       r.created_at, r.finished_at, r.confirmed_at, r.undone_at, r.skipped, (r.undo_requested_at IS NOT NULL)::boolean AS undoing,
       (SELECT count(*) FROM curation_actions a
         WHERE a.run_id = r.id AND a.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND books_age_visible(a.books, sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))::int AS actions,
       (SELECT count(*) FROM curation_actions a
         WHERE a.run_id = r.id AND a.state = 'undone'
           AND a.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND books_age_visible(a.books, sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))::int AS undone
FROM curation_runs r
LEFT JOIN users u ON u.id = r.started_by
WHERE r.id = sqlc.arg(id)
  AND (EXISTS (SELECT 1 FROM curation_actions a
               WHERE a.run_id = r.id AND a.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND books_age_visible(a.books, sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
       OR (r.started_by = sqlc.arg(viewer)::uuid AND NOT EXISTS (SELECT 1 FROM curation_actions a WHERE a.run_id = r.id)));

-- name: ListCurationActions :many
-- The run's actions in libraries the viewer sees, the first made first.
SELECT a.id, a.run_id, a.library_id, a.kind, a.rule, a.reason, a.books, a.trashed_files, a.before, a.after,
       a.state, a.note, a.created_at, a.undone_at
FROM curation_actions a
WHERE a.run_id = sqlc.arg(run_id)
  AND a.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND books_age_visible(a.books, sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)
  AND (sqlc.narg(after_at)::timestamptz IS NULL OR (a.created_at, a.id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY a.created_at, a.id
LIMIT sqlc.arg(max_rows);

-- name: GetCurationActionForUpdate :one
-- An action in a library the viewer sees, locked.
SELECT a.id, a.run_id, a.library_id, a.kind, a.books, a.trashed_files, a.before, a.after, a.state, a.created_at,
       r.automatic, r.state AS run_state, r.dry_run
FROM curation_actions a
JOIN curation_runs r ON r.id = a.run_id
WHERE a.id = sqlc.arg(id)
  AND a.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND books_age_visible(a.books, sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)
FOR UPDATE OF a;

-- name: ListCurationActionIDsToUndo :many
-- The run's actions the viewer sees that are not undone, the last made
-- first, before the one an earlier try of an undo stopped at.
SELECT a.id, a.created_at FROM curation_actions a
WHERE a.run_id = sqlc.arg(run_id) AND a.state = 'done'
  AND NOT EXISTS (SELECT 1 FROM curation_runs r WHERE r.id = a.run_id AND r.dry_run)
  AND a.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND books_age_visible(a.books, sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (a.created_at, a.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg(max_rows);

-- name: SetCurationActionUndone :exec
UPDATE curation_actions SET state = 'undone', note = '', undone_at = now(), undone_by = sqlc.arg(by) WHERE id = sqlc.arg(id);

-- name: SetCurationActionNote :exec
UPDATE curation_actions SET note = $2 WHERE id = $1;

-- name: SettleCurationRun :exec
-- A run all of whose actions are undone is undone.
UPDATE curation_runs r SET state = 'undone', undone_at = now()
WHERE r.id = $1 AND r.state <> 'undone' AND r.state <> 'running'
  AND EXISTS (SELECT 1 FROM curation_actions a WHERE a.run_id = r.id)
  AND NOT EXISTS (SELECT 1 FROM curation_actions a WHERE a.run_id = r.id AND a.state = 'done');

-- name: ConfirmCurationRun :execrows
UPDATE curation_runs SET state = 'confirmed', confirmed_at = now() WHERE id = $1 AND state = 'done';

-- name: ListCurationRunBooks :many
-- The books of the run's actions that are not undone, to have written.
SELECT DISTINCT unnest(a.books)::uuid AS book_id FROM curation_actions a
WHERE a.run_id = $1 AND a.state = 'done';

-- name: ListCurationTitles :many
SELECT id, title FROM books WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: ListCurationNames :many
-- What the ids of authors, series, publishers and tags are called.
SELECT 'author'::text AS kind, id, name FROM authors WHERE id = ANY(sqlc.arg(ids)::uuid[])
UNION ALL
SELECT 'series', id, name FROM series WHERE id = ANY(sqlc.arg(ids)::uuid[])
UNION ALL
SELECT 'publisher', id, name FROM publishers WHERE id = ANY(sqlc.arg(ids)::uuid[])
UNION ALL
SELECT 'tag', id, name FROM tags WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: ListFilesToRestore :many
-- Files with where they lie, if they are still records.
SELECT f.id, f.book_id, f.library_id, f.rel_path, f.trashed_at, l.root_path, l.writable
FROM book_files f JOIN libraries l ON l.id = f.library_id
WHERE f.id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY f.id;

-- name: FinishCurationRunByID :exec
UPDATE curation_runs SET state = 'done', finished_at = now() WHERE id = $1 AND state = 'running';

-- name: CurationRunHasBook :one
-- Whether an action of the run is about the book, undone or not.
SELECT EXISTS (SELECT 1 FROM curation_actions WHERE run_id = $1 AND $2::uuid = ANY(books));

-- name: CreateDuplicatesRun :one
-- A run of the duplicates rules, waiting for its job.
INSERT INTO curation_runs (kind, rule, started_by, automatic, dry_run, state, classes, scope_viewer, scope_sees_all)
VALUES ('duplicates', sqlc.arg(rule), sqlc.narg(started_by), true, sqlc.arg(dry_run), 'planned',
        sqlc.arg(classes)::text[], sqlc.narg(scope_viewer), sqlc.arg(scope_sees_all))
RETURNING id;

-- name: GetDuplicatesRunToWork :one
SELECT id, state, dry_run, classes, resume_after, scope_viewer, scope_sees_all, started_by
FROM curation_runs WHERE id = $1 AND kind = 'duplicates';

-- name: StartDuplicatesRun :exec
UPDATE curation_runs SET state = 'running' WHERE id = $1 AND state = 'planned';

-- name: AdvanceDuplicatesRun :exec
UPDATE curation_runs SET resume_after = sqlc.arg(after_id), skipped = skipped + sqlc.arg(skipped)::int WHERE id = sqlc.arg(id);

-- name: FinishDuplicatesRun :exec
UPDATE curation_runs SET state = 'done', finished_at = now() WHERE id = $1 AND state = 'running';

-- name: DropEmptySystemRun :exec
-- A run nobody started that found nothing to do leaves no trace.
DELETE FROM curation_runs r
WHERE r.id = $1 AND r.started_by IS NULL
  AND NOT EXISTS (SELECT 1 FROM curation_actions a WHERE a.run_id = r.id);

-- name: HasActiveDuplicatesRun :one
SELECT EXISTS (SELECT 1 FROM curation_runs WHERE kind = 'duplicates' AND automatic AND state IN ('planned', 'running'));

-- name: FirstActiveAdmin :one
SELECT id FROM users WHERE role = 'admin' AND disabled_at IS NULL ORDER BY created_at, id LIMIT 1;

-- name: RequestRunUndo :execrows
-- Asks for the run to be undone by its job; not while it still makes
-- actions, not twice, and not for a dry run, which changed nothing.
UPDATE curation_runs SET undo_requested_at = now(), undo_viewer = sqlc.arg(viewer), undo_sees_all = sqlc.arg(sees_all),
    undo_cursor_at = NULL, undo_cursor_id = NULL
WHERE id = sqlc.arg(id) AND undo_requested_at IS NULL AND NOT dry_run AND state NOT IN ('planned', 'running');

-- name: GetRunUndo :one
SELECT id, undo_requested_at, undo_viewer, undo_sees_all, undo_cursor_at, undo_cursor_id FROM curation_runs WHERE id = $1;

-- name: AdvanceRunUndo :exec
UPDATE curation_runs SET undo_cursor_at = $2, undo_cursor_id = $3 WHERE id = $1;

-- name: ClearRunUndo :exec
UPDATE curation_runs SET undo_requested_at = NULL WHERE id = $1;

-- name: GetCurationRunFlags :one
SELECT dry_run, state, undo_requested_at IS NOT NULL AS undoing FROM curation_runs WHERE id = $1;
