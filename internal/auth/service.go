package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin"
	"github.com/saybridge/saybridge/pkg/crypto"
	"github.com/saybridge/saybridge/pkg/metrics"
	"github.com/redis/go-redis/v9"
)

type authUseCase struct {
	repo   domain.UserRepository
	rdb    *redis.Client
	jwtMgr *crypto.JWTManager
	hooks  *plugin.HookRegistry
}

// NewAuthUseCase instantiates a new domain.AuthUseCase business logic service.
func NewAuthUseCase(repo domain.UserRepository, rdb *redis.Client, jwtMgr *crypto.JWTManager, hooks *plugin.HookRegistry) domain.AuthUseCase {
	return &authUseCase{
		repo:   repo,
		rdb:    rdb,
		jwtMgr: jwtMgr,
		hooks:  hooks,
	}
}

func (u *authUseCase) Register(ctx context.Context, username, email, password, displayName string) (*domain.User, error) {
	// 1. Fetch default workspace tenant (auto-bootstrapped if empty)
	tenant, err := u.repo.GetDefaultTenant(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve default workspace tenant: %w", err)
	}

	// 2. Validate email and username uniqueness
	existingEmail, _ := u.repo.GetUserByEmail(ctx, tenant.ID, email)
	if existingEmail != nil {
		return nil, errors.New("user with this email already exists")
	}

	existingUsername, _ := u.repo.GetUserByUsername(ctx, tenant.ID, username)
	if existingUsername != nil {
		return nil, errors.New("username is already taken")
	}

	// 3. Hash the raw password using secure Argon2id cryptography
	hashedPassword, err := crypto.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to secure password: %w", err)
	}

	// 4. Emit PreRegister lifecycle hook (invite-only, domain whitelist, captcha, etc.)
	// If any handler returns error, registration is halted.
	if err := u.hooks.Emit(ctx, plugin.PreRegister, map[string]interface{}{
		"username":     username,
		"email":        email,
		"display_name": displayName,
	}); err != nil {
		return nil, err
	}

	// 5. Construct the GORM entity
	user := &domain.User{
		TenantID:     tenant.ID,
		Username:     username,
		Email:        email,
		PasswordHash: hashedPassword,
		DisplayName:  displayName,
		SystemRole:   "user",
		Presence:     "offline",
		IsActive:     true,
	}

	// 6. Create user in the database (ACID transaction automatically creates UserSettings)
	if err := u.repo.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to persist user: %w", err)
	}

	// 7. Emit PostRegister lifecycle hook asynchronously (welcome email, analytics, etc.)
	u.hooks.EmitAsync(ctx, plugin.PostRegister, map[string]interface{}{
		"user_id":  user.ID,
		"username": user.Username,
		"email":    user.Email,
	})

	metrics.IncAuth("register")

	return user, nil
}

func (u *authUseCase) Login(ctx context.Context, email, password, deviceID, deviceName, ipAddress, userAgent string) (*domain.TokenPair, error) {
	// 1. Fetch default tenant
	tenant, err := u.repo.GetDefaultTenant(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve default workspace tenant: %w", err)
	}

	// 2. Find the user record
	user, err := u.repo.GetUserByEmail(ctx, tenant.ID, email)
	if err != nil {
		// Local user not found — try external authentication providers (LDAP, etc.)
		user, err = u.tryExternalAuth(ctx, email, password)
		if err != nil {
			return nil, errors.New("invalid email or password")
		}
	} else {
		// 3. Compare password hash using secure constant-time Argon2id comparison
		if !crypto.ComparePassword(password, user.PasswordHash) {
			// Local password mismatch — try external authentication as fallback
			user, err = u.tryExternalAuth(ctx, email, password)
			if err != nil {
				metrics.IncAuth("failure")
				return nil, errors.New("invalid email or password")
			}
		}
	}


	if !user.IsActive {
		return nil, errors.New("user account has been disabled")
	}

	// Emit PreLogin lifecycle hook (2FA, security policy checks, etc.)
	// If any handler returns error, the login flow is halted.
	if err := u.hooks.Emit(ctx, plugin.PreLogin, map[string]interface{}{
		"user_id": user.ID,
	}); err != nil {
		return nil, err
	}

	// 4. Generate asymmetric RS256 JWT Access Token (15 minutes expiry)
	accessToken, err := u.jwtMgr.GenerateAccessToken(user.ID, tenant.ID, user.SystemRole, deviceID, 15*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	// 5. Generate Opaque secure Refresh Token (UUID v4)
	refreshToken := uuid.New().String()

	// 6. Persist Refresh Token mapping inside Redis with a 30-day TTL (session expiration)
	redisKey := fmt.Sprintf("refresh_token:%s", refreshToken)
	redisValue := fmt.Sprintf("%s:%s", user.ID, deviceID)
	err = u.rdb.Set(ctx, redisKey, redisValue, 30*24*time.Hour).Err()
	if err != nil {
		return nil, fmt.Errorf("failed to store refresh token in cache: %w", err)
	}

	// Emit PostLogin lifecycle hook asynchronously (session management, audit logging, etc.)
	// Errors are logged but do not block the login response.
	u.hooks.EmitAsync(ctx, plugin.PostLogin, map[string]interface{}{
		"user_id":     user.ID,
		"device_id":   deviceID,
		"device_name": deviceName,
		"ip_address":  ipAddress,
		"user_agent":  userAgent,
	})

	// 7. Update last active timestamp and online status
	now := time.Now()
	user.LastActiveAt = &now
	user.Presence = "online"
	_ = u.repo.UpdateUser(ctx, user)

	metrics.IncAuth("success")

	return &domain.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    900, // 15 minutes in seconds
	}, nil
}

func (u *authUseCase) Refresh(ctx context.Context, refreshToken, deviceID string) (*domain.TokenPair, error) {
	// 1. Query Redis for the session token
	redisKey := fmt.Sprintf("refresh_token:%s", refreshToken)
	val, err := u.rdb.Get(ctx, redisKey).Result()
	if err != nil {
		return nil, errors.New("invalid or expired session token")
	}

	// Parse stored values
	parts := strings.Split(val, ":")
	if len(parts) != 2 {
		return nil, errors.New("corrupted session token data")
	}

	storedUserID := parts[0]
	storedDeviceID := parts[1]

	// Enforce strict device lock binding match
	if storedDeviceID != deviceID {
		return nil, errors.New("session device binding mismatch")
	}

	// Check if this session has been revoked in Redis
	revKey := fmt.Sprintf("revoked_session:%s:%s", storedUserID, storedDeviceID)
	exists, err := u.rdb.Exists(ctx, revKey).Result()
	if err == nil && exists > 0 {
		return nil, errors.New("this session has been revoked")
	}

	// 2. Fetch User metadata
	user, err := u.repo.GetUserByID(ctx, storedUserID)
	if err != nil {
		return nil, errors.New("user not found")
	}

	if !user.IsActive {
		return nil, errors.New("user account is currently deactivated")
	}

	// 3. Generate New Access Token
	newAccessToken, err := u.jwtMgr.GenerateAccessToken(user.ID, user.TenantID, user.SystemRole, deviceID, 15*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to rotate access token: %w", err)
	}

	// 4. Generate New Refresh Token (Refresh Token Rotation - RTR)
	newRefreshToken := uuid.New().String()

	// 5. Delete old Refresh Token to prevent replay attacks
	u.rdb.Del(ctx, redisKey)

	// 6. Save new session mapping to Redis cache cluster
	newRedisKey := fmt.Sprintf("refresh_token:%s", newRefreshToken)
	newRedisValue := fmt.Sprintf("%s:%s", user.ID, deviceID)
	err = u.rdb.Set(ctx, newRedisKey, newRedisValue, 30*24*time.Hour).Err()
	if err != nil {
		return nil, fmt.Errorf("failed to save rotated session token: %w", err)
	}

	metrics.IncAuth("refresh")

	return &domain.TokenPair{
		AccessToken:  newAccessToken,
		RefreshToken: newRefreshToken,
		ExpiresIn:    900,
	}, nil
}

// Logout instantly revokes the session on Redis by deleting the Refresh Token key.
func (u *authUseCase) Logout(ctx context.Context, refreshToken string) error {
	// 1. Extract user info from token before deletion (for audit logging)
	redisKey := fmt.Sprintf("refresh_token:%s", refreshToken)
	val, _ := u.rdb.Get(ctx, redisKey).Result()

	// 2. Delete the refresh token from Redis
	if err := u.rdb.Del(ctx, redisKey).Err(); err != nil {
		return err
	}

	// 3. Emit OnLogout lifecycle hook asynchronously (audit, session cleanup, etc.)
	userID := ""
	if parts := strings.Split(val, ":"); len(parts) >= 1 {
		userID = parts[0]
	}
	u.hooks.EmitAsync(ctx, plugin.OnLogout, map[string]interface{}{
		"user_id":       userID,
		"refresh_token": refreshToken,
	})

	return nil
}

// tryExternalAuth attempts to authenticate via external providers (LDAP, etc.)
// using the plugin hook registry. Returns the authenticated user or an error.
func (u *authUseCase) tryExternalAuth(ctx context.Context, email, password string) (*domain.User, error) {
	if !u.hooks.HasHandlers(plugin.AuthenticateExternal) {
		return nil, errors.New("no external auth providers available")
	}
	result, err := u.hooks.EmitCollect(ctx, plugin.AuthenticateExternal, map[string]interface{}{
		"email":    email,
		"password": password,
	})
	if err != nil {
		return nil, fmt.Errorf("external auth failed: %w", err)
	}
	if result == nil {
		return nil, errors.New("external auth returned no result")
	}
	extUser, ok := result.(*domain.User)
	if !ok {
		return nil, errors.New("external auth returned invalid user type")
	}
	return extUser, nil
}
