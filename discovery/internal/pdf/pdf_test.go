package pdf

import (
	"testing"
)

func TestGenerateKey(t *testing.T) {
	d := &Downloader{accountID: "test-account"}

	tests := []struct {
		name     string
		pdfURL   string
		rfpID    int
		wantKey  string
	}{
		{
			name:    "simple filename",
			pdfURL:  "https://example.com/docs/rfp-2024.pdf",
			rfpID:   123,
			wantKey: "pdfs/123/rfp-2024.pdf",
		},
		{
			name:    "filename with spaces",
			pdfURL:  "https://example.com/docs/RFP Document 2024.pdf",
			rfpID:   456,
			wantKey: "pdfs/456/RFP_Document_2024.pdf",
		},
		{
			name:    "no extension",
			pdfURL:  "https://example.com/download/document",
			rfpID:   789,
			wantKey: "pdfs/789/document.pdf",
		},
		{
			name:   "query parameters",
			pdfURL: "https://example.com/download.php?file=proposal.pdf&id=123",
			rfpID:  100,
			// URL parsing will get filename from path, not query params
			wantKey: "pdfs/100/download.php.pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.generateKey(tt.pdfURL, tt.rfpID)
			if got != tt.wantKey {
				t.Errorf("generateKey() = %q, want %q", got, tt.wantKey)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
	}{
		{
			name:  "clean filename",
			input: "document.pdf",
			want:  "document.pdf",
		},
		{
			name:  "spaces",
			input: "my document.pdf",
			want:  "my_document.pdf",
		},
		{
			name:  "special characters",
			input: "file:name?.pdf",
			want:  "file_name_.pdf",
		},
		{
			name:  "slashes",
			input: "path/to/file.pdf",
			want:  "path_to_file.pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}
