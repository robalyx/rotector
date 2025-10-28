package commands

import (
	"errors"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/cloudflare"
	"github.com/robalyx/rotector/internal/database"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

var (
	ErrNameRequired   = errors.New("NAME argument required")
	ErrReasonRequired = errors.New("REASON argument required")
	ErrTimeRequired   = errors.New("TIME argument required")
)

// CLIDependencies holds the common dependencies needed by CLI commands.
type CLIDependencies struct {
	DB       database.Client
	Migrator *migrate.Migrator
	CFClient *cloudflare.Client
	RoAPI    *api.API
	Logger   *zap.Logger
}
