package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin"
)

type fileUseCase struct {
	fileRepo    domain.FileRepository
	storageRepo domain.StorageRepository
	roomRepo    domain.RoomRepository
	userRepo    domain.UserRepository
	hooks       *plugin.HookRegistry
}

// NewFileUseCase instantiates a new domain.FileUseCase implementation.
func NewFileUseCase(
	fileRepo domain.FileRepository,
	storageRepo domain.StorageRepository,
	roomRepo domain.RoomRepository,
	userRepo domain.UserRepository,
	hooks *plugin.HookRegistry,
) domain.FileUseCase {
	return &fileUseCase{
		fileRepo:    fileRepo,
		storageRepo: storageRepo,
		roomRepo:    roomRepo,
		userRepo:    userRepo,
		hooks:       hooks,
	}
}

func (u *fileUseCase) PresignUpload(
	ctx context.Context,
	tenantID, userID, roomID, filename, contentType string,
	size int64,
) (string, string, string, error) {
	// 1. Get Quota Configurations
	maxFileSize := int64(50 * 1024 * 1024) // Default 50MB
	if envMaxFile := os.Getenv("MAX_FILE_UPLOAD_MB"); envMaxFile != "" {
		if val, err := strconv.ParseInt(envMaxFile, 10, 64); err == nil {
			maxFileSize = val * 1024 * 1024
		}
	}

	maxTenantStorage := int64(10 * 1024 * 1024 * 1024) // Default 10GB
	if envMaxStorage := os.Getenv("MAX_TENANT_STORAGE_GB"); envMaxStorage != "" {
		if val, err := strconv.ParseInt(envMaxStorage, 10, 64); err == nil {
			maxTenantStorage = val * 1024 * 1024 * 1024
		}
	}

	// 2. Validate File Size
	if size <= 0 {
		return "", "", "", errors.New("invalid file size")
	}
	if size > maxFileSize {
		return "", "", "", fmt.Errorf("file size exceeds maximum limit of %d MB", maxFileSize/(1024*1024))
	}

	// 3. Check Tenant Storage Quota
	currentStorage, err := u.fileRepo.GetTotalSizeByTenant(ctx, tenantID)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to check current tenant storage usage: %w", err)
	}

	if currentStorage+size > maxTenantStorage {
		return "", "", "", fmt.Errorf("tenant storage quota exceeded (%d GB limit)", maxTenantStorage/(1024*1024*1024))
	}

	// 4. Verify User Membership if RoomID is specified
	if roomID != "" {
		_, err := u.roomRepo.GetRoomMember(ctx, roomID, userID)
		if err != nil {
			return "", "", "", errors.New("permission denied: not a member of the target room")
		}
	}

	// 5. Generate Object Key
	ext := filepath.Ext(filename)
	objectKey := fmt.Sprintf("%s/%s%s", tenantID, uuid.New().String(), ext)

	// 6. Generate presigned URLs
	// Generates link valid for 15 minutes
	uploadURL, downloadURL, err := u.storageRepo.PresignUpload(ctx, objectKey, 15*80) // approx 20 mins
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate presign upload url: %w", err)
	}

	// 7. Save metadata as pending
	var roomIDPtr *string
	if roomID != "" {
		roomIDPtr = &roomID
	}

	fileRecord := &domain.File{
		BaseModel: domain.BaseModel{
			ID: uuid.New().String(),
		},
		TenantID:    tenantID,
		RoomID:      roomIDPtr,
		UserID:      userID,
		StorageKey:  objectKey,
		Filename:    filename,
		Size:        size,
		ContentType: contentType,
		Status:      "pending",
	}

	if err := u.fileRepo.Save(ctx, fileRecord); err != nil {
		return "", "", "", fmt.Errorf("failed to save file metadata: %w", err)
	}

	return uploadURL, downloadURL, fileRecord.ID, nil
}

// UploadFileDirect receives file binary from the backend handler, uploads it to
// MinIO via StorageRepository.UploadFile, saves metadata, and marks as completed.
func (u *fileUseCase) UploadFileDirect(
	ctx context.Context,
	tenantID, userID, roomID, filename, contentType string,
	size int64,
	reader io.Reader,
) (string, error) {
	// 1. Get Quota Configurations
	maxFileSize := int64(50 * 1024 * 1024) // Default 50MB
	if envMaxFile := os.Getenv("MAX_FILE_UPLOAD_MB"); envMaxFile != "" {
		if val, err := strconv.ParseInt(envMaxFile, 10, 64); err == nil {
			maxFileSize = val * 1024 * 1024
		}
	}

	maxTenantStorage := int64(10 * 1024 * 1024 * 1024) // Default 10GB
	if envMaxStorage := os.Getenv("MAX_TENANT_STORAGE_GB"); envMaxStorage != "" {
		if val, err := strconv.ParseInt(envMaxStorage, 10, 64); err == nil {
			maxTenantStorage = val * 1024 * 1024 * 1024
		}
	}

	// 2. Validate File Size
	if size <= 0 {
		return "", errors.New("invalid file size")
	}
	if size > maxFileSize {
		return "", fmt.Errorf("file size exceeds maximum limit of %d MB", maxFileSize/(1024*1024))
	}

	// 3. Check Tenant Storage Quota
	currentStorage, err := u.fileRepo.GetTotalSizeByTenant(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("failed to check current tenant storage usage: %w", err)
	}
	if currentStorage+size > maxTenantStorage {
		return "", fmt.Errorf("tenant storage quota exceeded (%d GB limit)", maxTenantStorage/(1024*1024*1024))
	}

	// 4. Verify User Membership if RoomID is specified
	if roomID != "" {
		_, err := u.roomRepo.GetRoomMember(ctx, roomID, userID)
		if err != nil {
			return "", errors.New("permission denied: not a member of the target room")
		}
	}

	// 5. Generate Object Key
	ext := filepath.Ext(filename)
	objectKey := fmt.Sprintf("%s/%s%s", tenantID, uuid.New().String(), ext)

	// 6. Upload binary to MinIO
	_, err = u.storageRepo.UploadFile(ctx, objectKey, reader, size, contentType)
	if err != nil {
		return "", fmt.Errorf("failed to upload file to storage: %w", err)
	}

	// 7. Save metadata as completed (no confirm step needed)
	var roomIDPtr *string
	if roomID != "" {
		roomIDPtr = &roomID
	}

	fileRecord := &domain.File{
		BaseModel: domain.BaseModel{
			ID: uuid.New().String(),
		},
		TenantID:    tenantID,
		RoomID:      roomIDPtr,
		UserID:      userID,
		StorageKey:  objectKey,
		Filename:    filename,
		Size:        size,
		ContentType: contentType,
		Status:      "completed",
	}

	if err := u.fileRepo.Save(ctx, fileRecord); err != nil {
		return "", fmt.Errorf("failed to save file metadata: %w", err)
	}

	// Emit OnFileUploaded hook asynchronously
	u.hooks.EmitAsync(ctx, plugin.OnFileUploaded, map[string]interface{}{
		"file_id":      fileRecord.ID,
		"tenant_id":    fileRecord.TenantID,
		"room_id":      fileRecord.RoomID,
		"user_id":      fileRecord.UserID,
		"filename":     fileRecord.Filename,
		"object_key":   fileRecord.StorageKey,
		"size":         fileRecord.Size,
		"content_type": fileRecord.ContentType,
	})

	return fileRecord.ID, nil
}

func (u *fileUseCase) ConfirmUpload(ctx context.Context, tenantID, userID, fileID string) error {
	file, err := u.fileRepo.FindByID(ctx, fileID)
	if err != nil {
		return errors.New("file not found")
	}

	if file.TenantID != tenantID {
		return errors.New("permission denied: tenant mismatch")
	}

	if file.UserID != userID {
		return errors.New("permission denied: only the uploader can confirm the upload")
	}

	if err := u.fileRepo.UpdateStatus(ctx, fileID, "completed"); err != nil {
		return fmt.Errorf("failed to confirm file upload: %w", err)
	}

	// Emit OnFileUploaded hook asynchronously
	u.hooks.EmitAsync(ctx, plugin.OnFileUploaded, map[string]interface{}{
		"file_id":      file.ID,
		"tenant_id":    file.TenantID,
		"room_id":      file.RoomID,
		"user_id":      file.UserID,
		"filename":     file.Filename,
		"object_key":   file.StorageKey,
		"size":         file.Size,
		"content_type": file.ContentType,
	})

	return nil
}

func (u *fileUseCase) ListRoomFiles(
	ctx context.Context,
	tenantID, userID, roomID string,
	limit, offset int,
) ([]domain.File, error) {
	// Verify user membership in the room before listing files
	_, err := u.roomRepo.GetRoomMember(ctx, roomID, userID)
	if err != nil {
		return nil, errors.New("permission denied: not a member of the room")
	}

	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	return u.fileRepo.FindByRoomID(ctx, tenantID, roomID, limit, offset)
}

func (u *fileUseCase) ListUserFiles(
	ctx context.Context,
	tenantID, userID string,
	limit, offset int,
) ([]domain.File, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return u.fileRepo.FindByUserID(ctx, tenantID, userID, limit, offset)
}

func (u *fileUseCase) ListAllTenantFiles(
	ctx context.Context,
	tenantID, userID string,
	limit, offset int,
) ([]domain.File, error) {
	usr, err := u.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, errors.New("user not found")
	}
	if usr.SystemRole != "admin" {
		return nil, errors.New("permission denied: admin access required")
	}

	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return u.fileRepo.FindAllTenantFiles(ctx, tenantID, limit, offset)
}

func (u *fileUseCase) ListSharedFiles(
	ctx context.Context,
	tenantID, userID string,
	limit, offset int,
) ([]domain.File, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return u.fileRepo.FindSharedFiles(ctx, tenantID, userID, limit, offset)
}

func (u *fileUseCase) DeleteFile(ctx context.Context, tenantID, userID, fileID string) error {
	file, err := u.fileRepo.FindByID(ctx, fileID)
	if err != nil {
		return errors.New("file not found")
	}

	if file.TenantID != tenantID {
		return errors.New("permission denied: tenant mismatch")
	}

	// Only owner or room owners/admins can delete files. Here we enforce owner check for simplicity.
	if file.UserID != userID {
		// Verify if the user is a room owner
		if file.RoomID != nil && *file.RoomID != "" {
			member, err := u.roomRepo.GetRoomMember(ctx, *file.RoomID, userID)
			if err != nil || member.RoomRole != "owner" {
				return errors.New("permission denied: you do not have permission to delete this file")
			}
		} else {
			return errors.New("permission denied: you do not have permission to delete this file")
		}
	}

	// Delete from MinIO
	if err := u.storageRepo.DeleteFile(ctx, file.StorageKey); err != nil {
		// Log warning but proceed to delete DB metadata to prevent dangling records
		fmt.Printf("[FileUseCase] Failed to delete object %s from storage: %v\n", file.StorageKey, err)
	}

	// Delete from database
	if err := u.fileRepo.Delete(ctx, fileID); err != nil {
		return fmt.Errorf("failed to delete file metadata: %w", err)
	}

	return nil
}

func (u *fileUseCase) GetFileByID(ctx context.Context, tenantID, userID, fileID string) (*domain.File, error) {
	file, err := u.fileRepo.FindByID(ctx, fileID)
	if err != nil {
		return nil, errors.New("file not found")
	}

	if file.TenantID != tenantID {
		return nil, errors.New("permission denied: tenant mismatch")
	}

	// Verify if the user is a member of the room where the file was shared (if it is a room file)
	if file.RoomID != nil && *file.RoomID != "" {
		_, err := u.roomRepo.GetRoomMember(ctx, *file.RoomID, userID)
		if err != nil {
			return nil, errors.New("permission denied: not a member of the room where the file is shared")
		}
	} else if file.UserID != userID {
		// If it is a personal file, only the owner can access it
		return nil, errors.New("permission denied: only the owner can access this file")
	}

	return file, nil
}

func (u *fileUseCase) GetSenderName(ctx context.Context, userID string) (string, error) {
	usr, err := u.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return "Unknown User", err
	}
	if usr.DisplayName != "" {
		return usr.DisplayName, nil
	}
	return usr.Username, nil
}

