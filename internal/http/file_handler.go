package httphandler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/response"
)

type FileHandler struct {
	fileUseCase domain.FileUseCase
	storageRepo domain.StorageRepository
}

func NewFileHandler(fileUseCase domain.FileUseCase, storageRepo domain.StorageRepository) *FileHandler {
	return &FileHandler{
		fileUseCase: fileUseCase,
		storageRepo: storageRepo,
	}
}

type presignRequest struct {
	Filename    string `json:"filename" binding:"required"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size" binding:"required"`
	RoomID      string `json:"room_id"`
}

// PresignUpload generates a pre-signed URL for file upload and registers metadata.
// @Summary Get presigned upload URL
// @Description Generate a presigned URL for uploading a file to S3-compatible storage and register metadata in database
// @Tags Files
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body presignRequest true "File upload details"
// @Success 200 {object} response.SuccessResponse "Presigned upload and download URLs"
// @Failure 400 {object} response.ErrorResponse "Invalid input"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 500 {object} response.ErrorResponse "Internal error"
// @Router /api/v1/files/presign [post]
func (h *FileHandler) PresignUpload(c *gin.Context) {
	var req presignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "BAD_REQUEST",
				"message": err.Error(),
				"status":  http.StatusBadRequest,
			},
		})
		return
	}

	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "UNAUTHORIZED",
				"message": "Tenant contexts missing from claims",
				"status":  http.StatusUnauthorized,
			},
		})
		return
	}

	userIDVal, _ := c.Get("user_id")
	userID, _ := userIDVal.(string)

	uploadURL, _, fileID, err := h.fileUseCase.PresignUpload(
		c.Request.Context(),
		tenantID.(string),
		userID,
		req.RoomID,
		req.Filename,
		req.ContentType,
		req.Size,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INTERNAL_ERROR",
				"message": err.Error(),
				"status":  http.StatusInternalServerError,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"id":           fileID,
			"upload_url":   uploadURL,
			"download_url": "/api/v1/files/download/" + fileID + "?filename=" + url.QueryEscape(req.Filename),
		},
	})
}

// UploadFile accepts multipart form-data, reads binary file, and uploads to MinIO via backend.
// POST /api/v1/files/upload
func (h *FileHandler) UploadFile(c *gin.Context) {
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Tenant context missing")
		return
	}

	userIDVal, _ := c.Get("user_id")
	userID, _ := userIDVal.(string)

	// Parse multipart form — max 50MB in memory
	if err := c.Request.ParseMultipartForm(50 << 20); err != nil {
		response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Failed to parse multipart form: "+err.Error())
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Missing 'file' field in multipart form")
		return
	}
	defer file.Close()

	roomID := c.Request.FormValue("room_id")
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	fileID, err := h.fileUseCase.UploadFileDirect(
		c.Request.Context(),
		tenantID.(string),
		userID,
		roomID,
		header.Filename,
		contentType,
		header.Size,
		file,
	)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "UPLOAD_ERROR", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{
		"id":           fileID,
		"filename":     header.Filename,
		"size":         header.Size,
		"content_type": contentType,
		"url":          "/api/v1/files/download/" + fileID + "?filename=" + url.QueryEscape(header.Filename),
	})
}

// ConfirmUpload transitions file status to completed after client uploads to MinIO.
// POST /api/v1/files/:id/confirm
func (h *FileHandler) ConfirmUpload(c *gin.Context) {
	fileID := c.Param("id")
	if fileID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "File ID is required")
		return
	}

	tenantID, exists := c.Get("tenant_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Tenant context missing")
		return
	}

	userIDVal, _ := c.Get("user_id")
	userID, _ := userIDVal.(string)

	if err := h.fileUseCase.ConfirmUpload(c.Request.Context(), tenantID.(string), userID, fileID); err != nil {
		response.Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{"success": true})
}

// DownloadFile proxies file stream from MinIO to hide host and verify authorization.
// GET /api/v1/files/download/:id
func (h *FileHandler) DownloadFile(c *gin.Context) {
	fileID := c.Param("id")
	if fileID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "File ID is required")
		return
	}

	tenantID, exists := c.Get("tenant_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Tenant context missing")
		return
	}

	userIDVal, _ := c.Get("user_id")
	userID, _ := userIDVal.(string)

	file, err := h.fileUseCase.GetFileByID(c.Request.Context(), tenantID.(string), userID, fileID)
	if err != nil {
		response.Error(c, http.StatusForbidden, "FORBIDDEN", err.Error())
		return
	}

	if file.Status != "completed" {
		response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "File upload is not confirmed yet")
		return
	}

	stream, err := h.storageRepo.GetFileStream(c.Request.Context(), file.StorageKey)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "STORAGE_ERROR", err.Error())
		return
	}
	defer stream.Close()

	// Detect inline displaying (images, audio, video) vs attachment downloading (others)
	inline := false
	if file.ContentType != "" {
		if strings.HasPrefix(file.ContentType, "image/") ||
			strings.HasPrefix(file.ContentType, "audio/") ||
			strings.HasPrefix(file.ContentType, "video/") ||
			file.ContentType == "application/pdf" {
			inline = true
		}
	}

	disposition := "attachment"
	if inline {
		disposition = "inline"
	}

	c.Header("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, file.Filename))
	c.Header("Content-Type", file.ContentType)
	c.Header("Content-Length", strconv.FormatInt(file.Size, 10))

	// Stream the object data to the client response
	if _, err := io.Copy(c.Writer, stream); err != nil {
		// Log streaming error
		fmt.Printf("[FileHandler] Error streaming file data: %v\n", err)
	}
}

// DeleteFile deletes file metadata and storage object.
// DELETE /api/v1/files/:id
func (h *FileHandler) DeleteFile(c *gin.Context) {
	fileID := c.Param("id")
	if fileID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "File ID is required")
		return
	}

	tenantID, exists := c.Get("tenant_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Tenant context missing")
		return
	}

	userIDVal, _ := c.Get("user_id")
	userID, _ := userIDVal.(string)

	if err := h.fileUseCase.DeleteFile(c.Request.Context(), tenantID.(string), userID, fileID); err != nil {
		response.Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{"success": true})
}

// ListRoomFiles returns files completed in a specific room context.
// GET /api/v1/rooms/:id/files
func (h *FileHandler) ListRoomFiles(c *gin.Context) {
	roomID := c.Param("id")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID is required")
		return
	}

	tenantID, exists := c.Get("tenant_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Tenant context missing")
		return
	}

	userIDVal, _ := c.Get("user_id")
	userID, _ := userIDVal.(string)

	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	files, err := h.fileUseCase.ListRoomFiles(c.Request.Context(), tenantID.(string), userID, roomID, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{
		"room_id": roomID,
		"files":   h.enrichFilesResponse(c, files),
	})
}

// ListUserFiles returns files uploaded by the current authenticated user.
// GET /api/v1/files/my
func (h *FileHandler) ListUserFiles(c *gin.Context) {
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Tenant context missing")
		return
	}

	userIDVal, _ := c.Get("user_id")
	userID, _ := userIDVal.(string)

	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	files, err := h.fileUseCase.ListUserFiles(c.Request.Context(), tenantID.(string), userID, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{
		"files": h.enrichFilesResponse(c, files),
	})
}

// ListSharedFiles returns files shared with the user in rooms they are members of.
// GET /api/v1/files/shared
func (h *FileHandler) ListSharedFiles(c *gin.Context) {
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Tenant context missing")
		return
	}

	userIDVal, _ := c.Get("user_id")
	userID, _ := userIDVal.(string)

	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	files, err := h.fileUseCase.ListSharedFiles(c.Request.Context(), tenantID.(string), userID, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{
		"files": h.enrichFilesResponse(c, files),
	})
}

// ListAllFiles returns all files inside this tenant. Admin access required.
// GET /api/v1/files/all
func (h *FileHandler) ListAllFiles(c *gin.Context) {
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Tenant context missing")
		return
	}

	userIDVal, _ := c.Get("user_id")
	userID, _ := userIDVal.(string)

	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	files, err := h.fileUseCase.ListAllTenantFiles(c.Request.Context(), tenantID.(string), userID, limit, offset)
	if err != nil {
		response.Error(c, http.StatusForbidden, "FORBIDDEN", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{
		"files": h.enrichFilesResponse(c, files),
	})
}

// Helper to construct response structures
func (h *FileHandler) enrichFilesResponse(c *gin.Context, files []domain.File) interface{} {
	type fileRespItem struct {
		ID          string    `json:"id"`
		Filename    string    `json:"filename"`
		ContentType string    `json:"content_type"`
		Size        int64     `json:"size"`
		URL         string    `json:"url"`
		SenderName  string    `json:"sender_name"`
		CreatedAt   time.Time `json:"created_at"`
	}

	enriched := make([]fileRespItem, 0, len(files))
	for _, f := range files {
		senderName, _ := h.fileUseCase.GetSenderName(c.Request.Context(), f.UserID)
		if senderName == "" {
			senderName = "Unknown User"
		}
		enriched = append(enriched, fileRespItem{
			ID:          f.ID,
			Filename:    f.Filename,
			ContentType: f.ContentType,
			Size:        f.Size,
			URL:         "/api/v1/files/download/" + f.ID + "?filename=" + url.QueryEscape(f.Filename),
			SenderName:  senderName,
			CreatedAt:   f.CreatedAt,
		})
	}
	return enriched
}
