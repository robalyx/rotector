package rpc

import (
	"context"
	"net/http"

	"github.com/rotector/rotector/internal/common/api/middleware/header"
	"github.com/rotector/rotector/internal/common/api/middleware/ip"
	"github.com/rotector/rotector/internal/common/api/middleware/ratelimit"
	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/rpc/handler"
	"github.com/rotector/rotector/internal/rpc/proto"
	"github.com/twitchtv/twirp"
	"go.uber.org/zap"
)

// Server implements the API service.
type Server struct {
	userHandler  *handler.UserHandler
	groupHandler *handler.GroupHandler
}

// NewServer creates a new server.
func NewServer(db *database.Client, logger *zap.Logger, config *config.APIConfig) (http.Handler, error) {
	// Create server
	server := &Server{
		userHandler:  handler.NewUserHandler(db, logger),
		groupHandler: handler.NewGroupHandler(db, logger),
	}

	// Create middleware
	headerMiddleware := header.New(logger)
	ipMiddleware := ip.New(logger, &config.IP)
	rateLimiter := ratelimit.New(&config.RateLimit, db, logger)

	// Create hooks
	hooks := twirp.ChainHooks(
		ipMiddleware.AsRPCHooks(),
		rateLimiter.AsRPCHooks(),
	)

	// Create server
	apiServer := proto.NewRotectorServiceServer(server, hooks)

	// Wrap with header extraction HTTP middleware
	return headerMiddleware.AsHTTPMiddleware(apiServer), nil
}

// GetUser implements the user service.
func (s *Server) GetUser(ctx context.Context, req *proto.GetUserRequest) (*proto.GetUserResponse, error) {
	return s.userHandler.GetUser(ctx, req)
}

// GetGroup implements the group service.
func (s *Server) GetGroup(ctx context.Context, req *proto.GetGroupRequest) (*proto.GetGroupResponse, error) {
	return s.groupHandler.GetGroup(ctx, req)
}
