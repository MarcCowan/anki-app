package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

// Settings are the scheduling settings shared by all devices.
type Settings struct {
	DesiredRetention float64
	DayRolloverHour  int
}

const (
	keyDesiredRetention = "desired_retention"
	keyDayRolloverHour  = "day_rollover_hour"
)

// GetSettings reads the settings seeded by the first migration.
func (s *Store) GetSettings(ctx context.Context) (Settings, error) {
	var out Settings
	var r, h string
	err := s.db.QueryRowContext(ctx,
		"SELECT (SELECT value FROM settings WHERE key = ?), (SELECT value FROM settings WHERE key = ?)",
		keyDesiredRetention, keyDayRolloverHour).Scan(&r, &h)
	if err != nil {
		return Settings{}, fmt.Errorf("get settings: %w", err)
	}
	if out.DesiredRetention, err = strconv.ParseFloat(r, 64); err != nil {
		return Settings{}, fmt.Errorf("get settings: %s: %w", keyDesiredRetention, err)
	}
	if out.DayRolloverHour, err = strconv.Atoi(h); err != nil {
		return Settings{}, fmt.Errorf("get settings: %s: %w", keyDayRolloverHour, err)
	}
	return out, nil
}

// UpdateSettings writes both settings in one transaction.
func (s *Store) UpdateSettings(ctx context.Context, st Settings) error {
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		const q = "INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"
		if _, err := tx.ExecContext(ctx, q, keyDesiredRetention, strconv.FormatFloat(st.DesiredRetention, 'f', -1, 64)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, q, keyDayRolloverHour, strconv.Itoa(st.DayRolloverHour))
		return err
	})
	if err != nil {
		return fmt.Errorf("update settings: %w", err)
	}
	return nil
}
