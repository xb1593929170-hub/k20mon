package request

import (
	"errors"
	"time"
)

type MetricsHistoryRequest struct {
	From  string `form:"from" json:"from"`
	To    string `form:"to" json:"to"`
	Limit int    `form:"limit" json:"limit"`
}

func (r MetricsHistoryRequest) Range(now time.Time) (time.Time, time.Time, int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	to := now
	from := now.Add(-24 * time.Hour)
	var err error
	if r.To != "" {
		to, err = time.Parse(time.RFC3339, r.To)
		if err != nil {
			return time.Time{}, time.Time{}, 0, errors.New("to must be RFC3339")
		}
	}
	if r.From != "" {
		from, err = time.Parse(time.RFC3339, r.From)
		if err != nil {
			return time.Time{}, time.Time{}, 0, errors.New("from must be RFC3339")
		}
	}
	if from.After(to) {
		return time.Time{}, time.Time{}, 0, errors.New("from must not be after to")
	}
	limit := r.Limit
	if limit <= 0 {
		// Five-second sampling produces about 17,280 rows per day. Keep the
		// default large enough for the dashboard's 24-hour summary.
		limit = 20_000
	}
	if limit > 600_000 {
		limit = 600_000
	}
	return from.UTC(), to.UTC(), limit, nil
}
