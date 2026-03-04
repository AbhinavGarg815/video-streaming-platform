package video

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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

type watchVariantResponse struct {
	Quality     string `json:"quality"`
	PlaylistURL string `json:"playlist_url"`
}

type watchResponse struct {
	VideoID           string                 `json:"video_id"`
	Title             string                 `json:"title"`
	Status            string                 `json:"status"`
	DurationSeconds   int64                  `json:"duration_seconds"`
	ThumbnailURL      string                 `json:"thumbnail_url"`
	MasterPlaylistURL string                 `json:"master_playlist_url"`
	Variants          []watchVariantResponse `json:"variants"`
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(r *gin.Engine, authMiddleware gin.HandlerFunc) {
	videos := r.Group("/upload")
	videos.Use(authMiddleware)
	videos.POST("/presign", h.PresignUpload)

	r.GET("/watch/:video_id", h.Watch)
	r.GET("/watch/:video_id/stream/*asset_path", h.StreamWatchAsset)
	r.GET("/watch/:video_id/player", h.WatchPlayer)
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

func (h *Handler) Watch(c *gin.Context) {
	videoID := strings.TrimSpace(c.Param("video_id"))
	if videoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "video_id is required"})
		return
	}

	playback, err := h.service.GetWatchPlayback(c.Request.Context(), videoID)
	if err != nil {
		switch {
		case errors.Is(err, ErrVideoNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		case errors.Is(err, ErrVideoNotReady):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		case errors.Is(err, ErrPlaybackNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
	}

	variants := make([]watchVariantResponse, 0, len(playback.Variants))
	for _, variant := range playback.Variants {
		variants = append(variants, watchVariantResponse{
			Quality:     variant.Quality,
			PlaylistURL: variant.PlaylistURL,
		})
	}

	c.JSON(http.StatusOK, watchResponse{
		VideoID:           playback.VideoID,
		Title:             playback.Title,
		Status:            playback.Status,
		DurationSeconds:   playback.DurationSeconds,
		ThumbnailURL:      playback.ThumbnailURL,
		MasterPlaylistURL: playback.MasterPlaylistURL,
		Variants:          variants,
	})
}

func (h *Handler) StreamWatchAsset(c *gin.Context) {
	videoID := strings.TrimSpace(c.Param("video_id"))
	if videoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "video_id is required"})
		return
	}

	assetPath := strings.TrimPrefix(strings.TrimSpace(c.Param("asset_path")), "/")
	if assetPath == "" {
		assetPath = "master.m3u8"
	}

	asset, err := h.service.OpenWatchAsset(c.Request.Context(), videoID, assetPath)
	if err != nil {
		switch {
		case errors.Is(err, ErrVideoNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		case errors.Is(err, ErrVideoNotReady):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		case errors.Is(err, ErrPlaybackNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
	}
	defer asset.Body.Close()

	if asset.ContentType != "" {
		c.Header("Content-Type", asset.ContentType)
	}
	c.Header("Cache-Control", "no-store")
	_, _ = io.Copy(c.Writer, asset.Body)
}

func (h *Handler) WatchPlayer(c *gin.Context) {
	videoID := strings.TrimSpace(c.Param("video_id"))
	if videoID == "" {
		c.String(http.StatusBadRequest, "video_id is required")
		return
	}

	html := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
	<meta charset="UTF-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1.0" />
	<title>Watch Video</title>
	<style>
		body { font-family: Arial, sans-serif; margin: 24px; background: #111; color: #eee; }
		.card { max-width: 900px; margin: 0 auto; }
		h1 { margin-bottom: 8px; }
		.meta { color: #bbb; margin-bottom: 12px; }
		video { width: 100%%; border-radius: 8px; background: black; }
		.controls { margin: 12px 0; display: flex; gap: 8px; align-items: center; }
		select { padding: 6px; }
		.err { color: #ff8686; margin-top: 12px; white-space: pre-wrap; }
	</style>
	<script src="https://cdn.jsdelivr.net/npm/hls.js@latest"></script>
</head>
<body>
	<div class="card">
		<h1 id="title">Loading...</h1>
		<div class="meta" id="meta"></div>
		<div class="controls">
			<label for="quality">Resolution:</label>
			<select id="quality"></select>
		</div>
		<video id="player" controls></video>
		<div class="err" id="err"></div>
	</div>

	<script>
		const videoID = %q;
		const player = document.getElementById('player');
		const titleEl = document.getElementById('title');
		const metaEl = document.getElementById('meta');
		const qualityEl = document.getElementById('quality');
		const errEl = document.getElementById('err');
		let playback = null;
		let hls = null;

		function setError(msg) { errEl.textContent = msg; }

		function setSource(url) {
			const resumeAt = player.currentTime || 0;
			if (hls) {
				hls.destroy();
				hls = null;
			}

			if (window.Hls && Hls.isSupported()) {
				hls = new Hls();
				hls.loadSource(url);
				hls.attachMedia(player);
				hls.on(Hls.Events.MANIFEST_PARSED, () => {
					if (resumeAt > 0) {
						try { player.currentTime = resumeAt; } catch (_) {}
					}
					player.play().catch(() => {});
				});
				hls.on(Hls.Events.ERROR, (_, data) => {
					if (data && data.fatal) {
						setError('HLS playback error: ' + data.type + ' - ' + data.details);
					}
				});
			} else if (player.canPlayType('application/vnd.apple.mpegurl')) {
				player.src = url;
				player.addEventListener('loadedmetadata', () => {
					if (resumeAt > 0) {
						try { player.currentTime = resumeAt; } catch (_) {}
					}
					player.play().catch(() => {});
				}, { once: true });
			} else {
				setError('This browser does not support HLS playback.');
			}
		}

		async function init() {
			try {
				const res = await fetch('/watch/' + encodeURIComponent(videoID));
				const body = await res.json();
				if (!res.ok) {
					setError(body.error || 'Failed to load watch metadata');
					titleEl.textContent = 'Error';
					return;
				}

				playback = body;
				titleEl.textContent = body.title || ('Video ' + body.video_id);
				metaEl.textContent = 'Status: ' + body.status + ' | Duration: ' + body.duration_seconds + 's';

				qualityEl.innerHTML = '';
				const autoOpt = document.createElement('option');
				autoOpt.value = body.master_playlist_url;
				autoOpt.textContent = 'Auto (Master)';
				qualityEl.appendChild(autoOpt);

				(body.variants || []).forEach((variant) => {
					const opt = document.createElement('option');
					opt.value = variant.playlist_url;
					opt.textContent = variant.quality;
					qualityEl.appendChild(opt);
				});

				qualityEl.addEventListener('change', () => setSource(qualityEl.value));
				setSource(body.master_playlist_url || (body.variants[0] && body.variants[0].playlist_url));
			} catch (e) {
				setError('Failed to initialize player: ' + e.message);
				titleEl.textContent = 'Error';
			}
		}

		init();
	</script>
</body>
</html>`, videoID)

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}
