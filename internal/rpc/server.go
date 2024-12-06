package rpc

import (
	"context"
	"net/http"

	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/rpc/handler"
	"github.com/rotector/rotector/internal/rpc/middleware/ip"
	"github.com/rotector/rotector/internal/rpc/middleware/ratelimit"
	"github.com/rotector/rotector/rpc/user"
	"github.com/twitchtv/twirp"
	"go.uber.org/zap"
)

// Server implements the user service.
type Server struct {
	db          *database.Client
	logger      *zap.Logger
	userHandler *handler.UserHandler
}

// NewServer creates a new user service server with middleware.
func NewServer(db *database.Client, logger *zap.Logger, config *config.RPCConfig) http.Handler {
	// Create server
	server := &Server{
		db:          db,
		logger:      logger,
		userHandler: handler.NewUserHandler(db, logger),
	}

	// Create middleware
	ipMiddleware := ip.New(logger, &config.IP)
	rateLimiter := ratelimit.New(&config.RateLimit)

	// Create Twirp server with chained hooks
	hooks := twirp.ChainHooks(
		ipMiddleware.ServerHooks(),
		rateLimiter.ServerHooks(),
	)

	// Create Twirp server and wrap with middleware
	twirpServer := user.NewUserServiceServer(server, hooks)
	return ip.WithHeaderExtraction(logger, &config.IP)(twirpServer)
}

// GetUser implements the GetUser RPC method.
func (s *Server) GetUser(ctx context.Context, req *user.GetUserRequest) (*user.GetUserResponse, error) {
	return s.userHandler.GetUser(ctx, req)
}
