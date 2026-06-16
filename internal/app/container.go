package app

import (
	"github.com/meilisearch/meilisearch-go"
	natspkg "github.com/nats-io/nats.go"
	goredis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/saybridge/saybridge/internal/domain"
	httphandler "github.com/saybridge/saybridge/internal/http"
	"github.com/saybridge/saybridge/internal/ws"
	"github.com/saybridge/saybridge/internal/plugin"
	"github.com/saybridge/saybridge/internal/repositories"
	"github.com/saybridge/saybridge/internal/stores"
	"github.com/saybridge/saybridge/internal/auth"
	"github.com/saybridge/saybridge/internal/file"
	"github.com/saybridge/saybridge/internal/message"
	"github.com/saybridge/saybridge/internal/room"
	"github.com/saybridge/saybridge/internal/search"
	"github.com/saybridge/saybridge/internal/user"
	"github.com/saybridge/saybridge/pkg/config"
	"github.com/saybridge/saybridge/pkg/crypto"
)

// Container is the central dependency injection container (Goravel-inspired).
// It holds all application-level dependencies organized by layer.
// Dependencies are initialized via 2-phase lifecycle: Register (repos) → Boot (usecases, handlers).
type Container struct {
	// ── Infrastructure ──────────────────────────────────────────────────
	DB       *gorm.DB
	RDB      *goredis.Client
	JS       natspkg.JetStreamContext
	NatsConn *natspkg.Conn
	Meili    meilisearch.ServiceManager
	JWTMgr   *crypto.JWTManager
	Cfg      *config.Config
	Hub      *ws.Hub

	// ── Repositories ────────────────────────────────────────────────────
	UserRepo     domain.UserRepository
	RoomRepo     domain.RoomRepository
	MessageRepo  domain.MessageRepository
	SearchRepo   domain.SearchRepository
	StorageRepo  domain.StorageRepository
	PresenceRepo domain.PresenceRepository
	ReadPosRepo  domain.ReadPositionRepository
	AuditRepo    domain.AuditLogRepository
	FileRepo     domain.FileRepository

	// ── Use Cases ───────────────────────────────────────────────────────
	AuthUC    domain.AuthUseCase
	UserUC    domain.UserUseCase
	RoomUC    domain.RoomUseCase
	MessageUC domain.MessageUseCase
	SearchUC  domain.SearchUseCase
	FileUC    domain.FileUseCase
	CallUC    interface{} // DEPRECATED: call is now a plugin — kept for Container struct compatibility

	// ── Handlers ────────────────────────────────────────────────────────
	AuthH      *httphandler.AuthHandler
	UserH      *httphandler.UserHandler
	RoomH      *httphandler.RoomHandler
	MessageH   *httphandler.MessageHandler
	SearchH    *httphandler.SearchHandler
	FileH      *httphandler.FileHandler
	PresenceH  *httphandler.PresenceHandler
	ReadPosH   *httphandler.ReadPositionHandler
	CallH      interface{}  // DEPRECATED: call is now a plugin
}

// NewContainer creates a new dependency container with infrastructure connections.
func NewContainer(
	cfg *config.Config,
	db *gorm.DB,
	rdb *goredis.Client,
	js natspkg.JetStreamContext,
	natsConn *natspkg.Conn,
	meili meilisearch.ServiceManager,
	jwtMgr *crypto.JWTManager,
) *Container {
	return &Container{
		DB:       db,
		RDB:      rdb,
		JS:       js,
		NatsConn: natsConn,
		Meili:    meili,
		JWTMgr:   jwtMgr,
		Cfg:      cfg,
	}
}

// Register initializes all repository implementations.
// Phase 1 of the 2-phase lifecycle (Goravel-style).
func (c *Container) Register() {
	c.UserRepo = repositories.NewPGUserRepository(c.DB)
	c.RoomRepo = repositories.NewPGRoomRepository(c.DB)
	c.MessageRepo = repositories.NewTimescaleMessageRepository(c.DB)
	c.SearchRepo = stores.NewSearchRepository(c.Meili)

	storageRepo, err := stores.NewMinioStorageRepository(c.Cfg)
	if err != nil {
		log.Warn().Err(err).Msg("[Container] MinIO storage init failed")
	}
	c.StorageRepo = storageRepo

	c.PresenceRepo = stores.NewRedisPresenceRepository(c.RDB)
	c.ReadPosRepo = repositories.NewPGReadPositionRepository(c.DB)
	c.AuditRepo = repositories.NewPGAuditLogRepository(c.DB)
	c.FileRepo = repositories.NewPGFileRepository(c.DB)
}

// Boot initializes use cases and handlers (depends on repositories).
// Phase 2 of the 2-phase lifecycle (Goravel-style).
func (c *Container) Boot() {
	// ── WebSocket Hub ─────────────────────────────────────────────────
	c.Hub = ws.NewHub(c.JS, c.NatsConn)
	go c.Hub.Run()

	// (Call is now initialized by plugins/call via OnServerStart hook)

	// ── Use Cases ─────────────────────────────────────────────────────
	c.AuthUC = auth.NewAuthUseCase(c.UserRepo, c.RDB, c.JWTMgr, plugin.Registry)
	c.UserUC = user.NewUserUseCase(c.UserRepo, plugin.Registry)
	c.RoomUC = room.NewRoomUseCase(c.RoomRepo, c.UserRepo, plugin.Registry)
	c.MessageUC = message.NewMessageUseCase(c.MessageRepo, c.UserRepo, c.RoomRepo, c.ReadPosRepo, c.JS, plugin.Registry)
	c.SearchUC = search.NewSearchUseCase(c.SearchRepo, c.UserRepo, c.RoomRepo)
	c.FileUC = file.NewFileUseCase(c.FileRepo, c.StorageRepo, c.RoomRepo, c.UserRepo, plugin.Registry)

	// ── Handlers ──────────────────────────────────────────────────────
	c.AuthH = httphandler.NewAuthHandler(c.AuthUC)
	c.UserH = httphandler.NewUserHandler(c.UserUC, c.StorageRepo)
	c.RoomH = httphandler.NewRoomHandler(c.RoomUC, c.MessageRepo, nil) // enforcer set separately in route config
	c.MessageH = httphandler.NewMessageHandler(c.MessageUC)
	c.SearchH = httphandler.NewSearchHandler(c.SearchUC)
	c.FileH = httphandler.NewFileHandler(c.FileUC, c.StorageRepo)
	c.PresenceH = httphandler.NewPresenceHandler(c.PresenceRepo, c.UserRepo, c.JS, plugin.Registry)
	c.ReadPosH = httphandler.NewReadPositionHandler(c.ReadPosRepo)
}
