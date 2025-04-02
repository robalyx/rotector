package types

import "time"

// MaterializedViewRefresh tracks when materialized views were last refreshed.
type MaterializedViewRefresh struct {
	ViewName    string    `bun:",pk"      json:"viewName"`
	LastRefresh time.Time `bun:",notnull" json:"lastRefresh"`
}
