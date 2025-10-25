package database

import (
	"github.com/robalyx/rotector/internal/database/service"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// Service provides access to all business logic services.
type Service struct {
	user     *service.UserService
	group    *service.GroupService
	reviewer *service.ReviewerService
	stats    *service.StatsService
	view     *service.ViewService
	sync     *service.SyncService
	comment  *service.CommentService
	cache    *service.CacheService
}

// NewService creates a new service instance with all services.
func NewService(db *bun.DB, repository *Repository, logger *zap.Logger) *Service {
	userModel := repository.User()
	groupModel := repository.Group()
	activityModel := repository.Activity()
	viewModel := repository.View()
	reviewerModel := repository.Reviewer()
	statsModel := repository.Stats()
	syncModel := repository.Sync()
	commentModel := repository.Comment()
	trackingModel := repository.Tracking()
	cacheModel := repository.Cache()

	viewService := service.NewView(viewModel, logger)

	return &Service{
		user:     service.NewUser(db, userModel, activityModel, trackingModel, logger),
		group:    service.NewGroup(db, groupModel, activityModel, logger),
		reviewer: service.NewReviewer(reviewerModel, viewService, logger),
		stats:    service.NewStats(statsModel, userModel, groupModel, logger),
		view:     viewService,
		sync:     service.NewSync(syncModel, logger),
		comment:  service.NewComment(commentModel, logger),
		cache:    service.NewCache(db, cacheModel, logger),
	}
}

// User returns the user service.
func (s *Service) User() *service.UserService {
	return s.user
}

// Group returns the group service.
func (s *Service) Group() *service.GroupService {
	return s.group
}

// Reviewer returns the reviewer service.
func (s *Service) Reviewer() *service.ReviewerService {
	return s.reviewer
}

// Stats returns the stats service.
func (s *Service) Stats() *service.StatsService {
	return s.stats
}

// View returns the view service.
func (s *Service) View() *service.ViewService {
	return s.view
}

// Sync returns the sync service.
func (s *Service) Sync() *service.SyncService {
	return s.sync
}

// Comment returns the comment service.
func (s *Service) Comment() *service.CommentService {
	return s.comment
}

// Cache returns the cache service.
func (s *Service) Cache() *service.CacheService {
	return s.cache
}
