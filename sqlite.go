package patchbin

import (
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

var sqliteSchema = `
CREATE TABLE IF NOT EXISTS app_users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  pubkey TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL UNIQUE,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS acl (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  pubkey string,
  ip_address string,
  permission string NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS patch_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  repo_name TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  text TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL,
  last_activity DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT pr_user_id_fk
    FOREIGN KEY(user_id) REFERENCES app_users(id)
    ON DELETE CASCADE
    ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS patchsets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  patch_request_id INTEGER NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT patchset_user_id_fk
    FOREIGN KEY(user_id) REFERENCES app_users(id)
    ON DELETE CASCADE
    ON UPDATE CASCADE,
  CONSTRAINT patchset_patch_request_id_fk
    FOREIGN KEY(patch_request_id) REFERENCES patch_requests(id)
    ON DELETE CASCADE
    ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS patches (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
	patchset_id INTEGER NOT NULL,
	author_name TEXT NOT NULL,
	author_email TEXT NOT NULL,
	author_date DATETIME NOT NULL,
	title TEXT NOT NULL,
	body TEXT NOT NULL,
	body_appendix TEXT NOT NULL,
	commit_sha TEXT NOT NULL,
	content_sha TEXT NOT NULL,
	raw_text TEXT NOT NULL,
	base_commit_sha TEXT,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	CONSTRAINT patches_user_id_fk
		FOREIGN KEY(user_id) REFERENCES app_users(id)
		ON DELETE CASCADE
		ON UPDATE CASCADE,
	CONSTRAINT patches_patchset_id_fk
		FOREIGN KEY(patchset_id) REFERENCES patchsets(id)
		ON DELETE CASCADE
		ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS event_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
	patch_request_id INTEGER,
	patchset_id INTEGER,
	event TEXT NOT NULL,
	data TEXT,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	CONSTRAINT event_logs_pr_id_fk
		FOREIGN KEY(patch_request_id) REFERENCES patch_requests(id)
		ON DELETE CASCADE
		ON UPDATE CASCADE,
	CONSTRAINT event_logs_patchset_id_fk
		FOREIGN KEY(patchset_id) REFERENCES patchsets(id)
		ON DELETE CASCADE
		ON UPDATE CASCADE,
	CONSTRAINT event_logs_user_id_fk
		FOREIGN KEY(user_id) REFERENCES app_users(id)
		ON DELETE CASCADE
		ON UPDATE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_patch_requests_last_activity ON patch_requests(last_activity);
`

var sqliteMigrations = []string{
	"", // migration #0 is reserved for schema initialization
	"ALTER TABLE patches ADD COLUMN base_commit_sha TEXT",
	// added this by accident
	"",
	// create repos table
	`CREATE TABLE IF NOT EXISTS repos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (user_id, name),
		CONSTRAINT repo_user_id_fk
			FOREIGN KEY(user_id) REFERENCES app_users(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE
	);`,
	// migrate existing repo info from patch_requests
	`INSERT INTO repos (user_id, name) SELECT user_id, repo_id from patch_requests group by repo_id;`,
	// convert patch_requests.repo_id to integer with FK constraint
	`CREATE TABLE IF NOT EXISTS tmp_patch_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		repo_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		text TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL,
		CONSTRAINT pr_user_id_fk
			FOREIGN KEY(user_id) REFERENCES app_users(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE,
		CONSTRAINT pr_repo_id_fk
			FOREIGN KEY(repo_id) REFERENCES repos(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE
	);
	INSERT INTO tmp_patch_requests (user_id, repo_id, name, text, status, created_at, updated_at)
		SELECT pr.user_id, repos.id, pr.name, pr.text, pr.status, pr.created_at, pr.updated_at
		FROM patch_requests AS pr
		INNER JOIN repos ON repos.name = pr.repo_id;
	DROP TABLE patch_requests;
	ALTER TABLE tmp_patch_requests RENAME TO patch_requests;`,
	// convert event_logs.repo_id to integer with FK constraint
	`CREATE TABLE IF NOT EXISTS tmp_event_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		repo_id INTEGER,
		patch_request_id INTEGER,
		patchset_id INTEGER,
		event TEXT NOT NULL,
		data TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT event_logs_pr_id_fk
			FOREIGN KEY(patch_request_id) REFERENCES patch_requests(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE,
		CONSTRAINT event_logs_patchset_id_fk
			FOREIGN KEY(patchset_id) REFERENCES patchsets(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE,
		CONSTRAINT event_logs_user_id_fk
			FOREIGN KEY(user_id) REFERENCES app_users(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE
		CONSTRAINT event_logs_repo_id_fk
			FOREIGN KEY(repo_id) REFERENCES repos(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE
	);
	INSERT INTO tmp_event_logs (user_id, repo_id, patch_request_id, patchset_id, event, data, created_at)
		SELECT ev.user_id, repos.id, ev.patch_request_id, ev.patchset_id, ev.event, ev.data, ev.created_at
		FROM event_logs AS ev
		LEFT JOIN repos ON repos.name = ev.repo_id;
	DROP TABLE event_logs;
	ALTER TABLE tmp_event_logs RENAME TO event_logs;`,
	// Phase 1: Add repo_name column to patch_requests
	`ALTER TABLE patch_requests ADD COLUMN repo_name TEXT`,
	// Phase 1: Populate repo_name from existing repos table
	`UPDATE patch_requests SET repo_name = (SELECT name FROM repos WHERE id = patch_requests.repo_id)`,
	// Phase 1: Remove patch_requests whose repo no longer exists. These are
	// orphans from repo deletion, since ON DELETE CASCADE never fired
	// because PRAGMA foreign_keys was never enabled.
	`DELETE FROM event_logs WHERE patch_request_id IN (SELECT id FROM patch_requests WHERE repo_name IS NULL);
	DELETE FROM patches WHERE patchset_id IN (SELECT id FROM patchsets WHERE patch_request_id IN (SELECT id FROM patch_requests WHERE repo_name IS NULL));
	DELETE FROM patchsets WHERE patch_request_id IN (SELECT id FROM patch_requests WHERE repo_name IS NULL);
	DELETE FROM patch_requests WHERE repo_name IS NULL;`,
	// Phase 1: Add last_activity column to patch_requests
	`ALTER TABLE patch_requests ADD COLUMN last_activity DATETIME`,
	// Phase 1: Set initial last_activity values from event_logs
	`UPDATE patch_requests SET last_activity = (SELECT MAX(created_at) FROM event_logs WHERE patch_request_id = patch_requests.id) WHERE last_activity IS NULL`,
	// Phase 1: Set last_activity to created_at for PRs with no events
	`UPDATE patch_requests SET last_activity = created_at WHERE last_activity IS NULL`,
	// Phase 1: Create index on last_activity for fast filtering
	`CREATE INDEX IF NOT EXISTS idx_patch_requests_last_activity ON patch_requests(last_activity)`,
	// Phase 2: Drop repos table (no longer needed, repo_name is stored directly)
	`DROP TABLE IF EXISTS repos`,
	// Phase 2: Rebuild patch_requests without repo_id (SQLite can't drop a
	// column that's part of a foreign key constraint via ALTER TABLE).
	`CREATE TABLE tmp_patch_requests_v2 (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		repo_name TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL,
		text TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL,
		last_activity DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT pr_user_id_fk
			FOREIGN KEY(user_id) REFERENCES app_users(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE
	);
	INSERT INTO tmp_patch_requests_v2 (id, user_id, repo_name, name, text, status, created_at, updated_at, last_activity)
		SELECT id, user_id, repo_name, name, text, status, created_at, updated_at, last_activity
		FROM patch_requests;
	DROP TABLE patch_requests;
	ALTER TABLE tmp_patch_requests_v2 RENAME TO patch_requests;
	CREATE INDEX IF NOT EXISTS idx_patch_requests_last_activity ON patch_requests(last_activity);`,
	// Phase 2: Rebuild event_logs without repo_id, same reasoning as above.
	`CREATE TABLE tmp_event_logs_v2 (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		patch_request_id INTEGER,
		patchset_id INTEGER,
		event TEXT NOT NULL,
		data TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT event_logs_pr_id_fk
			FOREIGN KEY(patch_request_id) REFERENCES patch_requests(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE,
		CONSTRAINT event_logs_patchset_id_fk
			FOREIGN KEY(patchset_id) REFERENCES patchsets(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE,
		CONSTRAINT event_logs_user_id_fk
			FOREIGN KEY(user_id) REFERENCES app_users(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE
	);
	INSERT INTO tmp_event_logs_v2 (id, user_id, patch_request_id, patchset_id, event, data, created_at)
		SELECT id, user_id, patch_request_id, patchset_id, event, data, created_at
		FROM event_logs;
	DROP TABLE event_logs;
	ALTER TABLE tmp_event_logs_v2 RENAME TO event_logs;`,
	// Phase 2: Collapse legacy statuses (closed, accepted, reviewed) into
	// open, since the new model only has draft and open.
	`UPDATE patch_requests SET status = 'open' WHERE status NOT IN ('draft', 'open')`,
	// Delete patch requests with an empty title. These come from patchsets
	// whose first patch had no subject line and are unusable in the UI.
	`DELETE FROM event_logs WHERE patch_request_id IN (SELECT id FROM patch_requests WHERE trim(name) = '');
	DELETE FROM patches WHERE patchset_id IN (SELECT id FROM patchsets WHERE patch_request_id IN (SELECT id FROM patch_requests WHERE trim(name) = ''));
	DELETE FROM patchsets WHERE patch_request_id IN (SELECT id FROM patch_requests WHERE trim(name) = '');
	DELETE FROM patch_requests WHERE trim(name) = '';`,
}

// Open opens a database connection.
func SqliteOpen(dsn string, logger *slog.Logger) (*sqlx.DB, error) {
	logger.Info("opening db file", "dsn", dsn)
	db, err := sqlx.Connect("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	err = sqliteUpgrade(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func sqliteUpgrade(db *sqlx.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("failed to query schema version: %v", err)
	}

	if version == len(sqliteMigrations) {
		return nil
	} else if version > len(sqliteMigrations) {
		return fmt.Errorf("patchbin (version %d) older than schema (version %d)", len(sqliteMigrations), version)
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if version == 0 {
		if _, err := tx.Exec(sqliteSchema); err != nil {
			return fmt.Errorf("failed to initialize schema: %v", err)
		}
	} else {
		for i := version; i < len(sqliteMigrations); i++ {
			if _, err := tx.Exec(sqliteMigrations[i]); err != nil {
				return fmt.Errorf("failed to execute migration #%v: %v", i, err)
			}
		}
	}

	// For some reason prepared statements don't work here
	_, err = tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", len(sqliteMigrations)))
	if err != nil {
		return fmt.Errorf("failed to bump schema version: %v", err)
	}

	return tx.Commit()
}
