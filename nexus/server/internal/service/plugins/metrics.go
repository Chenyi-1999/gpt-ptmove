package plugins

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

// Counts reads the metrics requested by installed manifests. Manifest values
// are matched to fixed queries and are never used as SQL identifiers.
func Counts(ctx context.Context, db *gorm.DB, snapshot Snapshot) map[string]*int64 {
	values := make(map[string]*int64)
	if db == nil {
		return values
	}
	var query string
	for _, manifest := range snapshot.Items {
		for _, b := range manifest.Blocks {
			if b.Kind != "metric" {
				continue
			}
			if _, visited := values[b.Metric]; visited {
				continue
			}
			switch b.Metric {
			case "visible_torrents":
				query = "SELECT count(*) FROM torrents WHERE is_hidden IS NULL OR is_hidden = 0"
			case "transfer_drafts":
				query = "SELECT count(*) FROM seed_parameters"
			case "configured_sites":
				query = "SELECT count(*) FROM sites"
			default:
				continue
			}
			var count sql.NullInt64
			values[b.Metric] = nil
			if err := db.WithContext(ctx).Raw(query).Row().Scan(&count); err == nil && count.Valid {
				values[b.Metric] = &count.Int64
			}
		}
	}
	return values
}
