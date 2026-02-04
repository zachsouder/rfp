// Package pdf provides PDF download and storage functionality.
package pdf

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/zachsouder/rfp/shared/r2"
)

const (
	downloadTimeout  = 60 * time.Second
	maxPDFSize       = 100 * 1024 * 1024 // 100MB
	userAgent        = "Mozilla/5.0 (compatible; RFPBot/1.0)"
)

// Downloader handles PDF download and storage.
type Downloader struct {
	r2Client  *r2.Client
	accountID string
}

// NewDownloader creates a new PDF downloader.
func NewDownloader(r2Client *r2.Client, accountID string) *Downloader {
	return &Downloader{
		r2Client:  r2Client,
		accountID: accountID,
	}
}

// DownloadResult contains the result of a PDF download operation.
type DownloadResult struct {
	SourceURL string `json:"source_url"`
	R2Key     string `json:"r2_key"`
	R2URL     string `json:"r2_url"`
	Size      int64  `json:"size"`
	Error     string `json:"error,omitempty"`
}

// DownloadAndStore downloads a PDF from a URL and stores it in R2.
func (d *Downloader) DownloadAndStore(ctx context.Context, pdfURL string, rfpID int) (*DownloadResult, error) {
	result := &DownloadResult{
		SourceURL: pdfURL,
	}

	// Generate R2 key
	r2Key := d.generateKey(pdfURL, rfpID)
	result.R2Key = r2Key

	// Check if already exists
	exists, err := d.r2Client.Exists(ctx, r2Key)
	if err == nil && exists {
		result.R2URL = d.r2Client.GetPublicURL(d.accountID, r2Key)
		return result, nil
	}

	// Download PDF
	body, size, err := d.downloadPDF(ctx, pdfURL)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	defer body.Close()

	result.Size = size

	// Upload to R2
	err = d.r2Client.Upload(ctx, r2Key, body, "application/pdf")
	if err != nil {
		result.Error = err.Error()
		return result, err
	}

	result.R2URL = d.r2Client.GetPublicURL(d.accountID, r2Key)
	return result, nil
}

// DownloadMultiple downloads multiple PDFs and returns results for each.
func (d *Downloader) DownloadMultiple(ctx context.Context, pdfURLs []string, rfpID int) []*DownloadResult {
	results := make([]*DownloadResult, 0, len(pdfURLs))

	for _, pdfURL := range pdfURLs {
		result, _ := d.DownloadAndStore(ctx, pdfURL, rfpID)
		results = append(results, result)
	}

	return results
}

// generateKey creates a unique R2 key for a PDF.
// Format: pdfs/{rfp_id}/{filename_hash}.pdf
func (d *Downloader) generateKey(pdfURL string, rfpID int) string {
	// Extract filename from URL
	parsedURL, err := url.Parse(pdfURL)
	if err != nil {
		// Fallback to hash of full URL
		hash := sha256.Sum256([]byte(pdfURL))
		return fmt.Sprintf("pdfs/%d/%x.pdf", rfpID, hash[:8])
	}

	// Get filename from path
	filename := path.Base(parsedURL.Path)
	if filename == "" || filename == "." || filename == "/" {
		// No filename in URL, use hash
		hash := sha256.Sum256([]byte(pdfURL))
		return fmt.Sprintf("pdfs/%d/%x.pdf", rfpID, hash[:8])
	}

	// Sanitize filename
	filename = sanitizeFilename(filename)

	// Ensure .pdf extension
	if !strings.HasSuffix(strings.ToLower(filename), ".pdf") {
		filename += ".pdf"
	}

	return fmt.Sprintf("pdfs/%d/%s", rfpID, filename)
}

// downloadPDF fetches a PDF from a URL.
func (d *Downloader) downloadPDF(ctx context.Context, pdfURL string) (io.ReadCloser, int64, error) {
	client := &http.Client{
		Timeout: downloadTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pdfURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/pdf,*/*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to download PDF: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("HTTP %d downloading PDF", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "pdf") && !strings.Contains(contentType, "octet-stream") {
		// Allow through anyway - some servers misconfigure content types
	}

	// Check size
	if resp.ContentLength > maxPDFSize {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("PDF too large: %d bytes (max %d)", resp.ContentLength, maxPDFSize)
	}

	// Wrap in size-limited reader
	limitedReader := io.LimitReader(resp.Body, maxPDFSize)
	return &limitedReadCloser{
		Reader: limitedReader,
		Closer: resp.Body,
	}, resp.ContentLength, nil
}

// limitedReadCloser wraps an io.Reader with a Close method.
type limitedReadCloser struct {
	io.Reader
	io.Closer
}

// sanitizeFilename removes problematic characters from filenames.
func sanitizeFilename(filename string) string {
	// Replace problematic characters
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		" ", "_",
	)
	filename = replacer.Replace(filename)

	// Limit length
	if len(filename) > 200 {
		ext := path.Ext(filename)
		filename = filename[:200-len(ext)] + ext
	}

	return filename
}
