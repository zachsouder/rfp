package handlers

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zachsouder/rfp/client/internal/middleware"
	"github.com/zachsouder/rfp/client/internal/workflow"
)

const (
	maxUploadSize = 10 << 20 // 10 MB
)

// Allowed file types for upload
var allowedContentTypes = map[string]bool{
	"application/pdf":                                                        true,
	"application/msword":                                                     true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	"application/vnd.ms-excel":                                               true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":      true,
	"image/jpeg":                                                             true,
	"image/png":                                                              true,
	"image/gif":                                                              true,
	"text/plain":                                                             true,
}

// UploadAttachment handles file uploads for RFPs.
func (h *Handlers) UploadAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.GetUser(ctx)
	idStr := chi.URLParam(r, "id")

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid RFP ID", http.StatusBadRequest)
		return
	}

	// Check if R2 is configured
	if h.r2Client == nil {
		slog.Error("attachment upload attempted but R2 not configured")
		http.Error(w, "File uploads are not configured", http.StatusServiceUnavailable)
		return
	}

	// Limit upload size
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	// Parse multipart form
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		slog.Error("failed to parse multipart form", "error", err)
		http.Error(w, "File too large (max 10 MB)", http.StatusBadRequest)
		return
	}

	// Get uploaded file
	file, header, err := r.FormFile("file")
	if err != nil {
		slog.Error("failed to get uploaded file", "error", err)
		http.Error(w, "No file uploaded", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate content type
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// Extract base content type (ignore charset etc)
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	if !allowedContentTypes[contentType] {
		http.Error(w, "File type not allowed", http.StatusBadRequest)
		return
	}

	// Ensure tracking record exists
	workflowSvc := workflow.NewService(h.db.Pool)
	_, err = workflowSvc.GetTracking(ctx, id)
	if err != nil {
		// Create a tracking record first
		if err := workflowSvc.UpdateStage(ctx, id, workflow.StageNew, user.ID); err != nil {
			slog.Error("failed to create tracking record", "error", err, "rfp_id", id)
			http.Error(w, "Failed to upload attachment", http.StatusInternalServerError)
			return
		}
	}

	// Get tracking ID
	tracking, err := workflowSvc.GetTracking(ctx, id)
	if err != nil {
		slog.Error("failed to get tracking record", "error", err, "rfp_id", id)
		http.Error(w, "Failed to upload attachment", http.StatusInternalServerError)
		return
	}

	// Generate unique file path
	ext := filepath.Ext(header.Filename)
	timestamp := time.Now().Unix()
	filePath := fmt.Sprintf("attachments/rfp-%d/%d-%s%s", id, timestamp, sanitizeFilename(header.Filename), ext)

	// Upload to R2
	if err := h.r2Client.Upload(ctx, filePath, file, contentType); err != nil {
		slog.Error("failed to upload to R2", "error", err, "rfp_id", id)
		http.Error(w, "Failed to upload file", http.StatusInternalServerError)
		return
	}

	// Store metadata in database
	_, err = h.db.Exec(ctx, `
		INSERT INTO client.attachments (rfp_tracking_id, uploaded_by, filename, file_path, file_size, content_type)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, tracking.ID, user.ID, header.Filename, filePath, header.Size, contentType)
	if err != nil {
		slog.Error("failed to save attachment metadata", "error", err, "rfp_id", id)
		// Try to clean up the uploaded file
		_ = h.r2Client.Delete(ctx, filePath)
		http.Error(w, "Failed to save attachment", http.StatusInternalServerError)
		return
	}

	slog.Info("attachment uploaded", "rfp_id", id, "filename", header.Filename, "size", header.Size, "user_id", user.ID)
	http.Redirect(w, r, "/rfps/"+idStr, http.StatusSeeOther)
}

// DownloadAttachment serves an attachment file.
func (h *Handlers) DownloadAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	attachmentIDStr := chi.URLParam(r, "attachmentId")

	rfpID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid RFP ID", http.StatusBadRequest)
		return
	}

	attachmentID, err := strconv.Atoi(attachmentIDStr)
	if err != nil {
		http.Error(w, "Invalid attachment ID", http.StatusBadRequest)
		return
	}

	// Check if R2 is configured
	if h.r2Client == nil {
		http.Error(w, "File downloads are not configured", http.StatusServiceUnavailable)
		return
	}

	// Fetch attachment metadata (verify it belongs to this RFP)
	var filename, filePath, contentType string
	err = h.db.QueryRow(ctx, `
		SELECT a.filename, a.file_path, a.content_type
		FROM client.attachments a
		JOIN client.rfp_tracking t ON a.rfp_tracking_id = t.id
		WHERE a.id = $1 AND t.discovery_rfp_id = $2
	`, attachmentID, rfpID).Scan(&filename, &filePath, &contentType)
	if err != nil {
		slog.Error("attachment not found", "error", err, "attachment_id", attachmentID, "rfp_id", rfpID)
		http.Error(w, "Attachment not found", http.StatusNotFound)
		return
	}

	// Download from R2
	body, err := h.r2Client.Download(ctx, filePath)
	if err != nil {
		slog.Error("failed to download from R2", "error", err, "file_path", filePath)
		http.Error(w, "Failed to download file", http.StatusInternalServerError)
		return
	}
	defer body.Close()

	// Set headers for download
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	// Stream file to response
	if _, err := io.Copy(w, body); err != nil {
		slog.Error("failed to stream file", "error", err)
	}
}

// sanitizeFilename removes potentially problematic characters from filenames.
func sanitizeFilename(name string) string {
	// Remove extension (we add it back separately)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	// Replace spaces and special chars
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
	// Limit length
	if len(name) > 50 {
		name = name[:50]
	}
	return name
}
