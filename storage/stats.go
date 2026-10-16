package storage

import "database/sql"

// Stats returns only the application pool counters. It does not ping or expose a pool.
func (db *DB) Stats() (sql.DBStats, error) {
	if db == nil || db.pool == nil || db.closed.Load() {
		return sql.DBStats{}, ErrClosed
	}
	stats := db.pool.Stats()
	if db.closed.Load() {
		return sql.DBStats{}, ErrClosed
	}
	return stats, nil
}

// Stats returns pool counters; closed handles explicitly report unavailable.
func (db *RuntimeDB) Stats() (sql.DBStats, error) {
	if db == nil {
		return sql.DBStats{}, ErrClosed
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed || db.pool == nil {
		return sql.DBStats{}, ErrClosed
	}
	return db.pool.Stats(), nil
}
