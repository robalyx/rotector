package database

import (
	"github.com/robalyx/rotector/internal/common/storage/database/service"
	"go.uber.org/zap"
)

// Service provides access to all business logic services.
type Service struct {
	ban        *service.BanService
	user       *service.UserService
	group      *service.GroupService
	vote       *service.VoteService
	reputation *service.ReputationService
	reviewer   *service.ReviewerService
	stats      *service.StatsService
	appeal     *service.AppealService
	view       *service.ViewService
	sync       *service.SyncService
	comment    *service.CommentService
}

// NewService creates a new service instance with all services.
func NewService(repository *Repository, logger *zap.Logger) *Service {
	banModel := repository.Ban()
	userModel := repository.User()
	groupModel := repository.Group()
	activityModel := repository.Activity()
	reputationModel := repository.Reputation()
	voteModel := repository.Vote()
	viewModel := repository.View()
	reviewerModel := repository.Reviewer()
	statsModel := repository.Stats()
	appealModel := repository.Appeal()
	syncModel := repository.Sync()
	commentModel := repository.Comment()

	viewService := service.NewView(viewModel, logger)

	return &Service{
		ban:        service.NewBan(banModel, logger),
		user:       service.NewUser(userModel, activityModel, reputationModel, voteModel, logger),
		group:      service.NewGroup(groupModel, activityModel, reputationModel, voteModel, logger),
		vote:       service.NewVote(voteModel, activityModel, viewService, banModel, logger),
		reputation: service.NewReputation(reputationModel, voteModel, logger),
		reviewer:   service.NewReviewer(reviewerModel, viewService, logger),
		stats:      service.NewStats(statsModel, userModel, groupModel, logger),
		appeal:     service.NewAppeal(appealModel, logger),
		view:       viewService,
		sync:       service.NewSync(syncModel, logger),
		comment:    service.NewComment(commentModel, logger),
	}
}

// Ban returns the ban service.
func (s *Service) Ban() *service.BanService {
	return s.ban
}

// User returns the user service.
func (s *Service) User() *service.UserService {
	return s.user
}

// Group returns the group service.
func (s *Service) Group() *service.GroupService {
	return s.group
}

// Vote returns the vote service.
func (s *Service) Vote() *service.VoteService {
	return s.vote
}

// Reputation returns the reputation service.
func (s *Service) Reputation() *service.ReputationService {
	return s.reputation
}

// Reviewer returns the reviewer service.
func (s *Service) Reviewer() *service.ReviewerService {
	return s.reviewer
}

// Stats returns the stats service.
func (s *Service) Stats() *service.StatsService {
	return s.stats
}

// Appeal returns the appeal service.
func (s *Service) Appeal() *service.AppealService {
	return s.appeal
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
