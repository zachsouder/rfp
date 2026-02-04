package r2

import (
	"testing"
)

func TestNewClient_MissingCredentials(t *testing.T) {
	tests := []struct {
		name            string
		accountID       string
		accessKeyID     string
		secretAccessKey string
		bucket          string
		wantErr         bool
	}{
		{
			name:            "all empty",
			accountID:       "",
			accessKeyID:     "",
			secretAccessKey: "",
			bucket:          "test",
			wantErr:         true,
		},
		{
			name:            "missing account ID",
			accountID:       "",
			accessKeyID:     "key",
			secretAccessKey: "secret",
			bucket:          "test",
			wantErr:         true,
		},
		{
			name:            "missing access key",
			accountID:       "account",
			accessKeyID:     "",
			secretAccessKey: "secret",
			bucket:          "test",
			wantErr:         true,
		},
		{
			name:            "missing secret key",
			accountID:       "account",
			accessKeyID:     "key",
			secretAccessKey: "",
			bucket:          "test",
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClient(tt.accountID, tt.accessKeyID, tt.secretAccessKey, tt.bucket)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetPublicURL(t *testing.T) {
	c := &Client{bucket: "my-bucket"}
	url := c.GetPublicURL("account123", "pdfs/1/doc.pdf")
	expected := "https://account123.r2.cloudflarestorage.com/my-bucket/pdfs/1/doc.pdf"
	if url != expected {
		t.Errorf("GetPublicURL() = %q, want %q", url, expected)
	}
}

func TestBucket(t *testing.T) {
	c := &Client{bucket: "test-bucket"}
	if got := c.Bucket(); got != "test-bucket" {
		t.Errorf("Bucket() = %q, want %q", got, "test-bucket")
	}
}
