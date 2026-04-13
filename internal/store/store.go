package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db *sql.DB
}

type Listing struct {
	ID        string    `json:"id"`
	Query     string    `json:"query"`
	Title     string    `json:"title"`
	Price     float64   `json:"price"`
	Currency  string    `json:"currency"`
	URL       string    `json:"url"`
	Condition string    `json:"condition"`
	Seller    string    `json:"seller"`
	Notified  bool      `json:"notified"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

type PricePoint struct {
	SeenAt time.Time `json:"seen_at"`
	Price  float64   `json:"price"`
	Query  string    `json:"query"`
}

type Watch struct {
	ID         int       `json:"id"`
	Query      string    `json:"query"`
	MaxPrice   float64   `json:"max_price"`
	CategoryID string    `json:"category_id"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

type Stats struct {
	TotalSeen     int        `json:"total_seen"`
	TotalNotified int        `json:"total_notified"`
	LowestPrice   float64    `json:"lowest_price"`
	AveragePrice  float64    `json:"average_price"`
	LastPollAt    *time.Time `json:"last_poll_at"`
}

func New(connStr string) (*Store, error) {
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	// Use a single connection with an advisory lock so concurrent
	// processes (e.g. parallel test packages) don't deadlock on DDL.
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(42)`); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, `SELECT pg_advisory_unlock(42)`) //nolint:errcheck

	_, err = conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS listings (
			id          TEXT PRIMARY KEY,
			query       TEXT NOT NULL,
			title       TEXT NOT NULL,
			price       DOUBLE PRECISION NOT NULL,
			currency    TEXT NOT NULL DEFAULT 'USD',
			url         TEXT NOT NULL,
			condition   TEXT NOT NULL DEFAULT '',
			seller      TEXT NOT NULL DEFAULT '',
			notified    BOOLEAN NOT NULL DEFAULT FALSE,
			first_seen  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_seen   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS price_history (
			id         SERIAL PRIMARY KEY,
			listing_id TEXT NOT NULL,
			query      TEXT NOT NULL,
			price      DOUBLE PRECISION NOT NULL,
			seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			FOREIGN KEY (listing_id) REFERENCES listings(id)
		);

		CREATE TABLE IF NOT EXISTS poll_log (
			id        SERIAL PRIMARY KEY,
			polled_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_price_history_query ON price_history(query, seen_at);
		CREATE INDEX IF NOT EXISTS idx_listings_query ON listings(query);
		CREATE INDEX IF NOT EXISTS idx_listings_notified ON listings(notified);

		CREATE TABLE IF NOT EXISTS watches (
			id          SERIAL PRIMARY KEY,
			query       TEXT NOT NULL,
			max_price   DOUBLE PRECISION NOT NULL,
			category_id TEXT NOT NULL DEFAULT '',
			enabled     BOOLEAN NOT NULL DEFAULT TRUE,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		ALTER TABLE watches ADD COLUMN IF NOT EXISTS category_id TEXT NOT NULL DEFAULT '';
	`)
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Truncate removes all data from all tables. Used by tests.
func (s *Store) Truncate() error {
	// Single TRUNCATE is atomic and acquires ACCESS EXCLUSIVE locks on all
	// tables at once, preventing cross-package test interference. CASCADE
	// handles FK dependencies.
	_, err := s.db.Exec(`TRUNCATE listings, price_history, poll_log, watches RESTART IDENTITY CASCADE`)
	return err
}

func (s *Store) HasSeen(id string) (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM listings WHERE id = $1`, id).Scan(&count)
	return count > 0, err
}

// UpsertListing records or updates a listing and appends a price history point.
func (s *Store) UpsertListing(l Listing) error {
	_, err := s.db.Exec(`
		INSERT INTO listings (id, query, title, price, currency, url, condition, seller, notified)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT(id) DO UPDATE SET
			price     = excluded.price,
			last_seen = NOW()
	`, l.ID, l.Query, l.Title, l.Price, l.Currency, l.URL, l.Condition, l.Seller, l.Notified)
	if err != nil {
		return fmt.Errorf("upsert listing: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO price_history (listing_id, query, price) VALUES ($1, $2, $3)`,
		l.ID, l.Query, l.Price,
	)
	return err
}

func (s *Store) MarkNotified(id string) error {
	_, err := s.db.Exec(`UPDATE listings SET notified = TRUE WHERE id = $1`, id)
	return err
}

func (s *Store) RecordPoll() error {
	_, err := s.db.Exec(`INSERT INTO poll_log (polled_at) VALUES (NOW())`)
	return err
}

func (s *Store) GetNotifiedListings(limit int) ([]Listing, error) {
	rows, err := s.db.Query(`
		SELECT id, query, title, price, currency, url, condition, seller, notified, first_seen, last_seen
		FROM listings
		WHERE notified = TRUE
		ORDER BY last_seen DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanListings(rows)
}

func (s *Store) GetPriceHistory(query string, days int) ([]PricePoint, error) {
	rows, err := s.db.Query(`
		SELECT seen_at, price, query
		FROM price_history
		WHERE query = $1
		  AND seen_at >= NOW() - make_interval(days => $2)
		ORDER BY seen_at ASC
	`, query, days)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var points []PricePoint
	for rows.Next() {
		var p PricePoint
		if err := rows.Scan(&p.SeenAt, &p.Price, &p.Query); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

func (s *Store) GetStats() (*Stats, error) {
	stats := &Stats{}

	err := s.db.QueryRow(`
		SELECT
			COUNT(*),
			COUNT(CASE WHEN notified = TRUE THEN 1 END),
			COALESCE(MIN(price), 0),
			COALESCE(AVG(price), 0)
		FROM listings
	`).Scan(&stats.TotalSeen, &stats.TotalNotified, &stats.LowestPrice, &stats.AveragePrice)
	if err != nil {
		return nil, err
	}

	var lastPoll time.Time
	err = s.db.QueryRow(
		`SELECT polled_at FROM poll_log ORDER BY polled_at DESC LIMIT 1`,
	).Scan(&lastPoll)
	if err == nil {
		stats.LastPollAt = &lastPoll
	}

	return stats, nil
}

// ── Watch CRUD ──────────────────────────────────────────────

func (s *Store) ListWatches() ([]Watch, error) {
	rows, err := s.db.Query(`SELECT id, query, max_price, category_id, enabled, created_at FROM watches ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanWatches(rows)
}

func (s *Store) ListEnabledWatches() ([]Watch, error) {
	rows, err := s.db.Query(`SELECT id, query, max_price, category_id, enabled, created_at FROM watches WHERE enabled = TRUE ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanWatches(rows)
}

func (s *Store) GetWatch(id int) (*Watch, error) {
	var w Watch
	err := s.db.QueryRow(
		`SELECT id, query, max_price, category_id, enabled, created_at FROM watches WHERE id = $1`, id,
	).Scan(&w.ID, &w.Query, &w.MaxPrice, &w.CategoryID, &w.Enabled, &w.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (s *Store) CreateWatch(query string, maxPrice float64, categoryID string) (*Watch, error) {
	var id int
	err := s.db.QueryRow(
		`INSERT INTO watches (query, max_price, category_id) VALUES ($1, $2, $3) RETURNING id`,
		query, maxPrice, categoryID,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create watch: %w", err)
	}
	return s.GetWatch(id)
}

func (s *Store) UpdateWatch(id int, query string, maxPrice float64, categoryID string, enabled bool) error {
	res, err := s.db.Exec(
		`UPDATE watches SET query = $1, max_price = $2, category_id = $3, enabled = $4 WHERE id = $5`,
		query, maxPrice, categoryID, enabled, id,
	)
	if err != nil {
		return fmt.Errorf("update watch: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("watch %d not found", id)
	}
	return nil
}

func (s *Store) DeleteWatch(id int) error {
	res, err := s.db.Exec(`DELETE FROM watches WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete watch: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("watch %d not found", id)
	}
	return nil
}

func scanWatches(rows *sql.Rows) ([]Watch, error) {
	var watches []Watch
	for rows.Next() {
		var w Watch
		if err := rows.Scan(&w.ID, &w.Query, &w.MaxPrice, &w.CategoryID, &w.Enabled, &w.CreatedAt); err != nil {
			return nil, err
		}
		watches = append(watches, w)
	}
	return watches, rows.Err()
}

func scanListings(rows *sql.Rows) ([]Listing, error) {
	var listings []Listing
	for rows.Next() {
		var l Listing
		err := rows.Scan(
			&l.ID, &l.Query, &l.Title, &l.Price, &l.Currency,
			&l.URL, &l.Condition, &l.Seller, &l.Notified,
			&l.FirstSeen, &l.LastSeen,
		)
		if err != nil {
			return nil, err
		}
		listings = append(listings, l)
	}
	return listings, rows.Err()
}
