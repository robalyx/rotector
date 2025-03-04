package database

import (
	"github.com/robalyx/rotector/internal/common/storage/database/models"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// Repository provides access to all database models.
type Repository struct {
	users      *models.UserModel
	groups     *models.GroupModel
	stats      *models.StatsModel
	settings   *models.SettingModel
	activities *models.ActivityModel
	tracking   *models.TrackingModel
	appeals    *models.AppealModel
	bans       *models.BanModel
	reputation *models.ReputationModel
	votes      *models.VoteModel
	views      *models.MaterializedViewModel
	consent    *models.ConsentModel
	reviewers  *models.ReviewerModel
	sync       *models.SyncModel
	message    *models.MessageModel
}

// NewRepository creates a new repository instance with all models.
func NewRepository(db *bun.DB, logger *zap.Logger) *Repository {
	// Initialize models in the correct order based on dependencies
	activities := models.NewActivity(db, logger)
	views := models.NewMaterializedView(db, logger)
	votes := models.NewVote(db, activities, views, logger)
	reputation := models.NewReputation(db, votes, logger)
	tracking := models.NewTracking(db, logger)
	users := models.NewUser(db, tracking, activities, reputation, votes, logger)
	groups := models.NewGroup(db, activities, reputation, votes, logger)
	consent := models.NewConsent(db, logger)
	reviewers := models.NewReviewer(db, views, logger)

	return &Repository{
		users:      users,
		groups:     groups,
		stats:      models.NewStats(db, users, groups, logger),
		settings:   models.NewSetting(db, logger),
		activities: activities,
		tracking:   tracking,
		appeals:    models.NewAppeal(db, logger),
		bans:       models.NewBan(db, logger),
		reputation: reputation,
		votes:      votes,
		views:      views,
		consent:    consent,
		reviewers:  reviewers,
		sync:       models.NewSync(db, logger),
		message:    models.NewMessage(db, logger),
	}
}

// Users returns the user model repository.
func (r *Repository) Users() *models.UserModel {
	return r.users
}

// Groups returns the group model repository.
func (r *Repository) Groups() *models.GroupModel {
	return r.groups
}

// Stats returns the stats model repository.
func (r *Repository) Stats() *models.StatsModel {
	return r.stats
}

// Settings returns the settings model repository.
func (r *Repository) Settings() *models.SettingModel {
	return r.settings
}

// Activities returns the activities model repository.
func (r *Repository) Activities() *models.ActivityModel {
	return r.activities
}

// Tracking returns the tracking model repository.
func (r *Repository) Tracking() *models.TrackingModel {
	return r.tracking
}

// Appeals returns the appeals model repository.
func (r *Repository) Appeals() *models.AppealModel {
	return r.appeals
}

// Bans returns the bans model repository.
func (r *Repository) Bans() *models.BanModel {
	return r.bans
}

// Reputation returns the reputation model repository.
func (r *Repository) Reputation() *models.ReputationModel {
	return r.reputation
}

// Votes returns the votes model repository.
func (r *Repository) Votes() *models.VoteModel {
	return r.votes
}

// Views returns the materialized views model repository.
func (r *Repository) Views() *models.MaterializedViewModel {
	return r.views
}

// Consent returns the consent model repository.
func (r *Repository) Consent() *models.ConsentModel {
	return r.consent
}

// Reviewers returns the reviewer model repository.
func (r *Repository) Reviewers() *models.ReviewerModel {
	return r.reviewers
}

// Sync returns the sync model repository.
func (r *Repository) Sync() *models.SyncModel {
	return r.sync
}

// Message returns the message model repository.
func (r *Repository) Message() *models.MessageModel {
	return r.message
}
