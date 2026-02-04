// Package email provides SMTP email sending and digest generation.
package email

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/smtp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is an interface for database query methods.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Config holds SMTP configuration.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
}

// Service provides email operations.
type Service struct {
	db     Querier
	config Config
}

// NewService creates a new email service.
func NewService(db Querier, config Config) *Service {
	return &Service{
		db:     db,
		config: config,
	}
}

// IsConfigured returns true if SMTP is configured.
func (s *Service) IsConfigured() bool {
	return s.config.Host != "" && s.config.From != ""
}

// DigestRFP represents an RFP in the digest email.
type DigestRFP struct {
	ID           int
	Title        string
	Agency       string
	State        string
	DueDate      string
	Score        string
	DiscoveredAt string
}

// DigestData holds data for the digest email template.
type DigestData struct {
	UserName    string
	RFPs        []DigestRFP
	Count       int
	PeriodStart string
	PeriodEnd   string
	BaseURL     string
}

// SendDigest sends a digest email to a single user.
func (s *Service) SendDigest(ctx context.Context, userEmail, userName string, data DigestData) error {
	if !s.IsConfigured() {
		return fmt.Errorf("email not configured")
	}

	subject := fmt.Sprintf("RFP Digest: %d New RFPs", data.Count)
	body, err := renderDigestTemplate(data)
	if err != nil {
		return fmt.Errorf("failed to render digest template: %w", err)
	}

	return s.send(userEmail, subject, body)
}

// send sends an email via SMTP.
func (s *Service) send(to, subject, htmlBody string) error {
	auth := smtp.PlainAuth("", s.config.User, s.config.Password, s.config.Host)

	headers := make(map[string]string)
	headers["From"] = s.config.From
	headers["To"] = to
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/html; charset=UTF-8"

	message := ""
	for k, v := range headers {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + htmlBody

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	err := smtp.SendMail(addr, auth, s.config.From, []string{to}, []byte(message))
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// SendDailyDigests sends digest emails to all subscribed users.
func (s *Service) SendDailyDigests(ctx context.Context, baseURL string) (int, error) {
	if !s.IsConfigured() {
		slog.Info("email not configured, skipping digests")
		return 0, nil
	}

	// Get subscribed users
	rows, err := s.db.Query(ctx, `
		SELECT u.id, u.email, COALESCE(u.first_name, '') as first_name
		FROM client.users u
		JOIN client.email_subscriptions es ON u.id = es.user_id
		WHERE es.digest_enabled = true
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to get subscribed users: %w", err)
	}
	defer rows.Close()

	type subscriber struct {
		ID        int
		Email     string
		FirstName string
	}

	var subscribers []subscriber
	for rows.Next() {
		var sub subscriber
		if err := rows.Scan(&sub.ID, &sub.Email, &sub.FirstName); err != nil {
			continue
		}
		subscribers = append(subscribers, sub)
	}

	if len(subscribers) == 0 {
		return 0, nil
	}

	// Get new RFPs from the last 24 hours
	rfps, err := s.getNewRFPs(ctx, 24*time.Hour)
	if err != nil {
		return 0, fmt.Errorf("failed to get new RFPs: %w", err)
	}

	if len(rfps) == 0 {
		slog.Info("no new RFPs for digest")
		return 0, nil
	}

	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)

	sent := 0
	for _, sub := range subscribers {
		userName := sub.FirstName
		if userName == "" {
			userName = "there"
		}

		data := DigestData{
			UserName:    userName,
			RFPs:        rfps,
			Count:       len(rfps),
			PeriodStart: yesterday.Format("Jan 2"),
			PeriodEnd:   now.Format("Jan 2, 2006"),
			BaseURL:     baseURL,
		}

		if err := s.SendDigest(ctx, sub.Email, userName, data); err != nil {
			slog.Error("failed to send digest", "error", err, "email", sub.Email)
			continue
		}

		sent++
	}

	slog.Info("sent digest emails", "count", sent, "rfps", len(rfps))
	return sent, nil
}

// getNewRFPs fetches RFPs discovered within the given duration.
func (s *Service) getNewRFPs(ctx context.Context, within time.Duration) ([]DigestRFP, error) {
	since := time.Now().Add(-within)

	rows, err := s.db.Query(ctx, `
		SELECT r.id, r.title, r.agency, r.state, r.due_date,
		       COALESCE(t.manual_score, t.auto_score) as score,
		       r.discovered_at
		FROM discovery.rfps r
		LEFT JOIN client.rfp_tracking t ON r.id = t.discovery_rfp_id
		WHERE r.is_active = true AND r.discovered_at >= $1
		ORDER BY COALESCE(t.manual_score, t.auto_score, 3) DESC, r.due_date ASC
		LIMIT 20
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rfps []DigestRFP
	for rows.Next() {
		var rfp DigestRFP
		var dueDate *time.Time
		var score *float64
		var discoveredAt time.Time

		if err := rows.Scan(&rfp.ID, &rfp.Title, &rfp.Agency, &rfp.State, &dueDate, &score, &discoveredAt); err != nil {
			continue
		}

		if dueDate != nil {
			rfp.DueDate = dueDate.Format("Jan 2, 2006")
		} else {
			rfp.DueDate = "Not specified"
		}

		if score != nil {
			rfp.Score = fmt.Sprintf("%.1f", *score)
		} else {
			rfp.Score = "-"
		}

		rfp.DiscoveredAt = discoveredAt.Format("Jan 2")
		rfps = append(rfps, rfp)
	}

	return rfps, nil
}

// digestTemplate is the HTML template for digest emails.
const digestTemplate = `
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #334155; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background: #1e40af; color: white; padding: 20px; border-radius: 8px 8px 0 0; }
        .content { background: #f8fafc; padding: 20px; border: 1px solid #e2e8f0; }
        .rfp-card { background: white; border: 1px solid #e2e8f0; border-radius: 8px; padding: 16px; margin-bottom: 12px; }
        .rfp-title { color: #1e40af; font-weight: 600; text-decoration: none; font-size: 16px; }
        .rfp-meta { color: #64748b; font-size: 14px; margin-top: 8px; }
        .score { display: inline-block; padding: 2px 8px; border-radius: 4px; font-weight: 600; font-size: 12px; }
        .score-high { background: #dcfce7; color: #166534; }
        .score-med { background: #fef9c3; color: #854d0e; }
        .score-low { background: #fee2e2; color: #991b1b; }
        .footer { padding: 20px; text-align: center; color: #64748b; font-size: 12px; }
        .btn { display: inline-block; background: #1e40af; color: white; padding: 12px 24px; border-radius: 6px; text-decoration: none; margin-top: 16px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1 style="margin: 0; font-size: 24px;">RFP Intelligence Digest</h1>
            <p style="margin: 8px 0 0; opacity: 0.9;">{{.PeriodStart}} - {{.PeriodEnd}}</p>
        </div>
        <div class="content">
            <p>Hi {{.UserName}},</p>
            <p>Here are <strong>{{.Count}} new RFPs</strong> discovered since your last digest:</p>

            {{range .RFPs}}
            <div class="rfp-card">
                <a href="{{$.BaseURL}}/rfps/{{.ID}}" class="rfp-title">{{.Title}}</a>
                <div class="rfp-meta">
                    <div>{{.Agency}} &bull; {{.State}}</div>
                    <div style="margin-top: 4px;">
                        Due: {{.DueDate}} &bull;
                        <span class="score {{if ge .Score "4"}}score-high{{else if ge .Score "3"}}score-med{{else}}score-low{{end}}">
                            Score: {{.Score}}
                        </span>
                    </div>
                </div>
            </div>
            {{end}}

            <div style="text-align: center;">
                <a href="{{.BaseURL}}/rfps" class="btn">View All RFPs</a>
            </div>
        </div>
        <div class="footer">
            <p>You're receiving this because you're subscribed to RFP digests.</p>
            <p>To unsubscribe, update your preferences in Settings.</p>
        </div>
    </div>
</body>
</html>
`

func renderDigestTemplate(data DigestData) (string, error) {
	tmpl, err := template.New("digest").Parse(digestTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
