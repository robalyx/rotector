package database

import (
	"github.com/robalyx/rotector/internal/database/models"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// Repository provides access to all database models.
type Repository struct {
	user     *models.UserModel
	group    *models.GroupModel
	stats    *models.StatsModel
	setting  *models.SettingModel
	activity *models.ActivityModel
	guildBan *models.GuildBanModel
	tracking *models.TrackingModel
	appeal   *models.AppealModel
	ban      *models.BanModel
	view     *models.MaterializedViewModel
	consent  *models.ConsentModel
	reviewer *models.ReviewerModel
	sync     *models.SyncModel
	message  *models.MessageModel
	condo    *models.CondoModel
	comment  *models.CommentModel
	ivan     *models.IvanModel
}

// NewRepository creates a new repository instance with all models.
func NewRepository(db *bun.DB, logger *zap.Logger) *Repository {
	return &Repository{
		user:     models.NewUser(db, logger),
		group:    models.NewGroup(db, logger),
		stats:    models.NewStats(db, logger),
		setting:  models.NewSetting(db, logger),
		activity: models.NewActivity(db, logger),
		guildBan: models.NewGuildBan(db, logger),
		tracking: models.NewTracking(db, logger),
		appeal:   models.NewAppeal(db, logger),
		ban:      models.NewBan(db, logger),
		view:     models.NewMaterializedView(db, logger),
		consent:  models.NewConsent(db, logger),
		reviewer: models.NewReviewer(db, logger),
		sync:     models.NewSync(db, logger),
		message:  models.NewMessage(db, logger),
		condo:    models.NewCondo(db, logger),
		comment:  models.NewComment(db, logger),
		ivan:     models.NewIvan(db, logger),
	}
}

// User returns the user model repository.
func (r *Repository) User() *models.UserModel {
	return r.user
}

// Group returns the group model repository.
func (r *Repository) Group() *models.GroupModel {
	return r.group
}

// Stats returns the stats model repository.
func (r *Repository) Stats() *models.StatsModel {
	return r.stats
}

// Setting returns the setting model repository.
func (r *Repository) Setting() *models.SettingModel {
	return r.setting
}

// Activity returns the activities model repository.
func (r *Repository) Activity() *models.ActivityModel {
	return r.activity
}

// GuildBan returns the guild ban model repository.
func (r *Repository) GuildBan() *models.GuildBanModel {
	return r.guildBan
}

// Tracking returns the tracking model repository.
func (r *Repository) Tracking() *models.TrackingModel {
	return r.tracking
}

// Appeal returns the appeal model repository.
func (r *Repository) Appeal() *models.AppealModel {
	return r.appeal
}

// Ban returns the ban model repository.
func (r *Repository) Ban() *models.BanModel {
	return r.ban
}

// View returns the materialized view model repository.
func (r *Repository) View() *models.MaterializedViewModel {
	return r.view
}

// Consent returns the consent model repository.
func (r *Repository) Consent() *models.ConsentModel {
	return r.consent
}

// Reviewer returns the reviewer model repository.
func (r *Repository) Reviewer() *models.ReviewerModel {
	return r.reviewer
}

// Sync returns the sync model repository.
func (r *Repository) Sync() *models.SyncModel {
	return r.sync
}

// Message returns the message model repository.
func (r *Repository) Message() *models.MessageModel {
	return r.message
}

// Condo returns the condo model repository.
func (r *Repository) Condo() *models.CondoModel {
	return r.condo
}

// Comment returns the comment model repository.
func (r *Repository) Comment() *models.CommentModel {
	return r.comment
}

// Ivan returns the ivan model repository.
func (r *Repository) Ivan() *models.IvanModel {
	return r.ivan
}
