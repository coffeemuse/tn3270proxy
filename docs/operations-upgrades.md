# Upgrading & database backups

tn3270proxy stores all state in a single SQLite file (`proxy.db` by default).
The schema is **versioned** and migrations are **forward-only**.

## What happens on upgrade

When a newer binary opens an older database it migrates the schema up
automatically. Before applying anything it writes a consistent snapshot next to
the database:

    proxy.db.pre-migrate-v<from>-<unixtime>.bak

Each migration runs in its own transaction and advances the recorded version
atomically, so an interrupted upgrade leaves the database at the last fully
applied version with the snapshot intact.

## Rolling back

There are no "down" migrations by design. To roll back an upgrade:

1. Stop the proxy.
2. Restore the snapshot: `mv proxy.db.pre-migrate-v<from>-<ts>.bak proxy.db`
   (move the live `proxy.db` aside first if you want to keep it).
3. Start the previous binary.

## Newer database, older binary

If you point an **older** binary at a database written by a **newer** one, it
refuses to start (`database schema is newer than this build`) rather than risk
corruption. Use the matching (or newer) binary, or restore a snapshot.

## On-demand backups

A backup can also be taken at any time via the store's `Backup` method (the
basis for the planned admin "Backup now" action). Snapshots are plain SQLite
files — verify one with `sqlite3 <file> "PRAGMA integrity_check;"`.

## Retention

Snapshots are **not** pruned automatically. They are git-ignored (`*.bak`);
remove old ones once an upgrade is confirmed healthy.
