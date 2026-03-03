package video

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/AbhinavGarg815/video-streaming-platform/internal/auth"
)

type Handler struct {
	service *Service
}

type presignUploadRequest struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
}

type presignUploadResponse struct {
	VideoID   string `json:"video_id"`
	UploadURL string `json:"upload_url"`
	ObjectKey string `json:"object_key"`
	Bucket    string `json:"bucket"`
	Method    string `json:"method"`
	ExpiresAt string `json:"expires_at"`
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(r *gin.Engine, authMiddleware gin.HandlerFunc) {
	videos := r.Group("/upload")
	videos.Use(authMiddleware)
	videos.POST("/presign", h.PresignUpload)
}

func (h *Handler) PresignUpload(c *gin.Context) {
	var req presignUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	userIDValue, exists := c.Get(auth.ContextUserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	userID, ok := userIDValue.(int64)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	presigned, err := h.service.CreatePresignedUpload(c.Request.Context(), userID, req.FileName, req.ContentType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, presignUploadResponse{
		VideoID:   presigned.VideoID,
		UploadURL: presigned.UploadURL,
		ObjectKey: presigned.ObjectKey,
		Bucket:    h.service.bucket,
		Method:    http.MethodPut,
		ExpiresAt: presigned.ExpiresAt.UTC().Format(time.RFC3339),
	})
}
