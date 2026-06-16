// Package server provides the application bootstrap, dependency injection container,
// and HTTP server lifecycle management with graceful shutdown.
package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/meilisearch/meilisearch-go"
	natspkg "github.com/nats-io/nats.go"
	goredis "github.com/redis/go-redis/v9"
	"github.com/saybridge/saybridge/internal/natsbus"
	"github.com/saybridge/saybridge/internal/repositories"
	"github.com/saybridge/saybridge/internal/stores"
	"github.com/saybridge/saybridge/pkg/config"
	"github.com/saybridge/saybridge/pkg/crypto"
	"gorm.io/gorm"
)

// Server holds all application-level dependencies and manages the HTTP server lifecycle.
type Server struct {
	cfg        *config.Config
	db         *gorm.DB
	rdb        *goredis.Client
	js         natspkg.JetStreamContext
	natsConn   *natspkg.Conn
	meili      meilisearch.ServiceManager
	jwtMgr     *crypto.JWTManager
	httpServer *http.Server
}

// New initializes all infrastructure connections and constructs a ready-to-run Server.
func New(cfg *config.Config) (*Server, error) {
	s := &Server{cfg: cfg}
	var err error

	// 1. PostgreSQL
	s.db, err = repositories.NewConnection(cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}

	// 2. Redis
	s.rdb, err = stores.NewConnection(cfg)
	if err != nil {
		return nil, fmt.Errorf("redis: %w", err)
	}

	// 3. TimescaleDB (uses same PostgreSQL connection — initialized later in router)

	// 4. NATS JetStream
	s.js, s.natsConn, err = natsbus.NewConnection(cfg)
	if err != nil {
		return nil, fmt.Errorf("nats: %w", err)
	}

	// 5. Meilisearch
	s.meili, err = stores.InitMeilisearch(cfg)
	if err != nil {
		return nil, fmt.Errorf("meilisearch: %w", err)
	}

	// 6. Search Indexer Worker
	searchRepo := stores.NewSearchRepository(s.meili)
	if err := stores.StartSearchIndexerWorker(s.js, searchRepo); err != nil {
		log.Warn().Err(err).Msg("[Server] Search indexer start failed")
	}

	// 7. JWT Manager (RSA keypair)
	if err := crypto.EnsureRSAKeysExist(cfg.JWTPrivateKeyPath, cfg.JWTPublicKeyPath); err != nil {
		return nil, fmt.Errorf("rsa keys: %w", err)
	}
	privKey, err := crypto.LoadPrivateKey(cfg.JWTPrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load private key: %w", err)
	}
	pubKey, err := crypto.LoadPublicKey(cfg.JWTPublicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load public key: %w", err)
	}
	s.jwtMgr = crypto.NewJWTManager(privKey, pubKey)

	return s, nil
}

// Run starts the HTTP server and blocks until a shutdown signal is received.
// Implements graceful shutdown with connection draining.
func (s *Server) Run() error {
	router := SetupRouter(s)

	s.httpServer = &http.Server{
		Addr:         ":" + s.cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Channel to receive OS signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start HTTP server in a goroutine
	errCh := make(chan error, 1)
	go func() {
		log.Info().Msgf("[Server] Starting REST API on port %s [%s mode]", s.cfg.Port, s.cfg.Env)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Block until signal or server error
	select {
	case sig := <-quit:
		log.Info().Msgf("[Server] Received signal %v, initiating graceful shutdown...", sig)
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	// Graceful shutdown with 30-second timeout for in-flight requests
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	s.Close()
	log.Info().Msg("[Server] Graceful shutdown completed")
	return nil
}

// Close releases all infrastructure resources in reverse-initialization order.
// Each resource is closed independently so a failure in one does not prevent others from closing.
func (s *Server) Close() {
	log.Info().Msg("[Server] Closing infrastructure connections...")

	// 1. Drain NATS subscriptions first (allows in-flight messages to complete)
	if s.natsConn != nil {
		log.Info().Msg("[Server] Draining NATS connection...")
		if err := s.natsConn.Drain(); err != nil {
			log.Error().Err(err).Msg("[Server] NATS drain error, forcing close")
			s.natsConn.Close()
		} else {
			log.Info().Msg("[Server] NATS drained and closed")
		}
	}

	// 2. (ScyllaDB removed — messages now in PostgreSQL/TimescaleDB)

	// 3. Close Redis connection pool
	if s.rdb != nil {
		log.Info().Msg("[Server] Closing Redis connection...")
		if err := s.rdb.Close(); err != nil {
			log.Error().Err(err).Msg("[Server] Redis close error")
		} else {
			log.Info().Msg("[Server] Redis connection closed")
		}
	}

	// 4. Close PostgreSQL connection pool (via GORM)
	if s.db != nil {
		log.Info().Msg("[Server] Closing PostgreSQL connection...")
		if sqlDB, err := s.db.DB(); err == nil {
			if err := sqlDB.Close(); err != nil {
				log.Error().Err(err).Msg("[Server] PostgreSQL close error")
			} else {
				log.Info().Msg("[Server] PostgreSQL connection closed")
			}
		}
	}

	log.Info().Msg("[Server] All infrastructure connections closed")
}
