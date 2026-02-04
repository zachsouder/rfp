package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zachsouder/rfp/client/internal/middleware"
	"github.com/zachsouder/rfp/client/internal/templates"
)

const (
	defaultPageSize = 25
	maxPageSize     = 100
)

// RFPListData contains data for the RFP list template.
type RFPListData struct {
	RFPs        []RFPListItem
	Filters     RFPFilters
	States      []string
	Categories  []string
	Page        int
	TotalPages  int
	TotalCount  int
	StartItem   int
	EndItem     int
	PageNumbers []int
}

// RFPListItem represents an RFP in the list view.
type RFPListItem struct {
	ID               int
	Title            string
	Agency           string
	State            string
	City             string
	DueDate          *time.Time
	DueDateFormatted string
	IsUrgent         bool
	Score            *float64
	Stage            string
	StageDisplay     string
}

// RFPFilters holds the current filter/sort state.
type RFPFilters struct {
	Stage    string
	State    string
	Category string
	Sort     string
}

// QueryString returns the filter parameters as a query string (without page).
func (f RFPFilters) QueryString() string {
	var parts []string
	if f.Stage != "" {
		parts = append(parts, "stage="+f.Stage)
	}
	if f.State != "" {
		parts = append(parts, "state="+f.State)
	}
	if f.Category != "" {
		parts = append(parts, "category="+f.Category)
	}
	if f.Sort != "" {
		parts = append(parts, "sort="+f.Sort)
	}
	if len(parts) == 0 {
		return ""
	}
	return "&" + strings.Join(parts, "&")
}

// RFPList renders the RFP list page.
func (h *Handlers) RFPList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.GetUser(ctx)

	// Parse query parameters
	filters := RFPFilters{
		Stage:    r.URL.Query().Get("stage"),
		State:    r.URL.Query().Get("state"),
		Category: r.URL.Query().Get("category"),
		Sort:     r.URL.Query().Get("sort"),
	}
	if filters.Sort == "" {
		filters.Sort = "due_date"
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	// Build query
	baseQuery := `
		SELECT r.id, r.title, r.agency, r.state, r.city, r.due_date, r.category,
		       COALESCE(t.manual_score, t.auto_score) as score, t.stage
		FROM discovery.rfps r
		LEFT JOIN client.rfp_tracking t ON r.id = t.discovery_rfp_id
		WHERE r.is_active = true AND (t.is_hidden IS NULL OR t.is_hidden = false)
	`

	countQuery := `
		SELECT COUNT(*)
		FROM discovery.rfps r
		LEFT JOIN client.rfp_tracking t ON r.id = t.discovery_rfp_id
		WHERE r.is_active = true AND (t.is_hidden IS NULL OR t.is_hidden = false)
	`

	var args []any
	argIdx := 1

	// Apply filters
	if filters.Stage != "" {
		baseQuery += fmt.Sprintf(" AND t.stage = $%d", argIdx)
		countQuery += fmt.Sprintf(" AND t.stage = $%d", argIdx)
		args = append(args, filters.Stage)
		argIdx++
	}
	if filters.State != "" {
		baseQuery += fmt.Sprintf(" AND r.state = $%d", argIdx)
		countQuery += fmt.Sprintf(" AND r.state = $%d", argIdx)
		args = append(args, filters.State)
		argIdx++
	}
	if filters.Category != "" {
		baseQuery += fmt.Sprintf(" AND r.category = $%d", argIdx)
		countQuery += fmt.Sprintf(" AND r.category = $%d", argIdx)
		args = append(args, filters.Category)
		argIdx++
	}

	// Count total
	var totalCount int
	if err := h.db.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		slog.Error("failed to count RFPs", "error", err)
	}

	// Apply sorting
	switch filters.Sort {
	case "score":
		baseQuery += " ORDER BY score DESC NULLS LAST"
	case "posted_date":
		baseQuery += " ORDER BY r.posted_date DESC NULLS LAST"
	case "title":
		baseQuery += " ORDER BY r.title ASC"
	default:
		baseQuery += " ORDER BY r.due_date ASC NULLS LAST"
	}

	// Apply pagination
	offset := (page - 1) * defaultPageSize
	baseQuery += fmt.Sprintf(" LIMIT %d OFFSET %d", defaultPageSize, offset)

	// Execute query
	rows, err := h.db.Query(ctx, baseQuery, args...)
	if err != nil {
		slog.Error("failed to query RFPs", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var rfps []RFPListItem
	now := time.Now()
	weekFromNow := now.AddDate(0, 0, 7)

	for rows.Next() {
		var item RFPListItem
		var category, stage *string
		if err := rows.Scan(&item.ID, &item.Title, &item.Agency, &item.State, &item.City,
			&item.DueDate, &category, &item.Score, &stage); err != nil {
			slog.Error("failed to scan RFP", "error", err)
			continue
		}
		if stage != nil {
			item.Stage = *stage
			item.StageDisplay = stageDisplayName(*stage)
		} else {
			item.Stage = "new"
			item.StageDisplay = "New"
		}
		if item.DueDate != nil {
			item.DueDateFormatted = item.DueDate.Format("Jan 2, 2006")
			item.IsUrgent = item.DueDate.Before(weekFromNow)
		}
		rfps = append(rfps, item)
	}

	// Get distinct states for filter dropdown
	states := h.getDistinctValues(ctx, "state")
	categories := h.getDistinctValues(ctx, "category")

	// Calculate pagination info
	totalPages := (totalCount + defaultPageSize - 1) / defaultPageSize
	if totalPages < 1 {
		totalPages = 1
	}

	startItem := offset + 1
	endItem := offset + len(rfps)
	if totalCount == 0 {
		startItem = 0
	}

	// Generate page numbers
	var pageNumbers []int
	for i := 1; i <= totalPages && i <= 10; i++ {
		pageNumbers = append(pageNumbers, i)
	}

	// Render template
	data := RFPListData{
		RFPs:        rfps,
		Filters:     filters,
		States:      states,
		Categories:  categories,
		Page:        page,
		TotalPages:  totalPages,
		TotalCount:  totalCount,
		StartItem:   startItem,
		EndItem:     endItem,
		PageNumbers: pageNumbers,
	}

	pageData := templates.PageData{
		Title:     "RFPs",
		ActiveNav: "rfps",
		User: &templates.User{
			ID:        user.ID,
			Email:     user.Email,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			IsAdmin:   user.IsAdmin(),
		},
		Data: data,
	}

	if err := h.templates.Render(w, "rfps", pageData); err != nil {
		slog.Error("failed to render RFPs template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// getDistinctValues fetches distinct non-null values for a column.
func (h *Handlers) getDistinctValues(ctx context.Context, column string) []string {
	query := fmt.Sprintf(`SELECT DISTINCT %s FROM discovery.rfps WHERE %s IS NOT NULL AND %s != '' ORDER BY %s`, column, column, column, column)
	rows, err := h.db.Query(ctx, query)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var val string
		if err := rows.Scan(&val); err == nil {
			values = append(values, val)
		}
	}
	return values
}

// stageDisplayName returns a human-readable stage name.
func stageDisplayName(stage string) string {
	switch stage {
	case "new":
		return "New"
	case "reviewing":
		return "Reviewing"
	case "qualified":
		return "Qualified"
	case "pursuing":
		return "Pursuing"
	case "submitted":
		return "Submitted"
	case "won":
		return "Won"
	case "lost":
		return "Lost"
	case "passed":
		return "Passed"
	default:
		return stage
	}
}

// RFPDetail renders the RFP detail page.
func (h *Handlers) RFPDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.GetUser(ctx)
	idStr := chi.URLParam(r, "id")

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid RFP ID", http.StatusBadRequest)
		return
	}

	// Fetch RFP details
	var rfp RFPDetailData
	var dueDate, postedDate *time.Time
	var category, venueType, portal, portalID, incumbent, loginNotes, sourceURL *string
	var termMonths *int
	var estimatedValue *float64

	err = h.db.QueryRow(ctx, `
		SELECT r.id, r.title, r.agency, r.state, r.city, r.due_date, r.posted_date,
		       r.category, r.venue_type, r.source_url, r.portal, r.portal_id,
		       r.term_months, r.estimated_value, r.incumbent, r.login_required, r.login_notes,
		       r.discovered_at, r.is_active
		FROM discovery.rfps r
		WHERE r.id = $1
	`, id).Scan(
		&rfp.ID, &rfp.Title, &rfp.Agency, &rfp.State, &rfp.City,
		&dueDate, &postedDate, &category, &venueType, &sourceURL,
		&portal, &portalID, &termMonths, &estimatedValue, &incumbent,
		&rfp.LoginRequired, &loginNotes, &rfp.DiscoveredAt, &rfp.IsActive,
	)
	if err != nil {
		slog.Error("failed to fetch RFP", "error", err, "id", id)
		http.Error(w, "RFP not found", http.StatusNotFound)
		return
	}

	if dueDate != nil {
		rfp.DueDate = dueDate.Format("January 2, 2006")
	}
	if postedDate != nil {
		rfp.PostedDate = postedDate.Format("January 2, 2006")
	}
	if category != nil {
		rfp.Category = *category
	}
	if venueType != nil {
		rfp.VenueType = *venueType
	}
	if sourceURL != nil {
		rfp.SourceURL = *sourceURL
	}
	if portal != nil {
		rfp.Portal = *portal
	}
	if portalID != nil {
		rfp.PortalID = *portalID
	}
	if termMonths != nil {
		rfp.TermMonths = *termMonths
	}
	if estimatedValue != nil {
		rfp.EstimatedValue = *estimatedValue
	}
	if incumbent != nil {
		rfp.Incumbent = *incumbent
	}
	if loginNotes != nil {
		rfp.LoginNotes = *loginNotes
	}

	// Fetch tracking info
	var stage *string
	var score *float64
	var priority *string
	err = h.db.QueryRow(ctx, `
		SELECT stage, COALESCE(manual_score, auto_score), priority
		FROM client.rfp_tracking
		WHERE discovery_rfp_id = $1
	`, id).Scan(&stage, &score, &priority)
	if err == nil {
		if stage != nil {
			rfp.Stage = *stage
			rfp.StageDisplay = stageDisplayName(*stage)
		}
		if score != nil {
			rfp.Score = *score
		}
		if priority != nil {
			rfp.Priority = *priority
		}
	} else {
		rfp.Stage = "new"
		rfp.StageDisplay = "New"
		rfp.Priority = "normal"
	}

	// Fetch notes for this RFP
	notesRows, err := h.db.Query(ctx, `
		SELECT n.id, COALESCE(u.first_name || ' ' || u.last_name, u.email) as author_name,
		       n.content, n.created_at
		FROM client.notes n
		JOIN client.rfp_tracking t ON n.rfp_tracking_id = t.id
		JOIN client.users u ON n.author_id = u.id
		WHERE t.discovery_rfp_id = $1
		ORDER BY n.created_at DESC
	`, id)
	if err != nil {
		slog.Error("failed to fetch notes", "error", err, "rfp_id", id)
	} else {
		defer notesRows.Close()
		for notesRows.Next() {
			var note Note
			var createdAt time.Time
			if err := notesRows.Scan(&note.ID, &note.AuthorName, &note.Content, &createdAt); err != nil {
				slog.Error("failed to scan note", "error", err)
				continue
			}
			note.CreatedAt = createdAt.Format("Jan 2, 2006 3:04 PM")
			rfp.Notes = append(rfp.Notes, note)
		}
	}

	// Fetch attachments for this RFP
	attachRows, err := h.db.Query(ctx, `
		SELECT a.id, a.filename, a.file_size, a.content_type,
		       COALESCE(u.first_name || ' ' || u.last_name, u.email) as uploaded_by,
		       a.created_at
		FROM client.attachments a
		JOIN client.rfp_tracking t ON a.rfp_tracking_id = t.id
		JOIN client.users u ON a.uploaded_by = u.id
		WHERE t.discovery_rfp_id = $1
		ORDER BY a.created_at DESC
	`, id)
	if err != nil {
		slog.Error("failed to fetch attachments", "error", err, "rfp_id", id)
	} else {
		defer attachRows.Close()
		for attachRows.Next() {
			var att Attachment
			var fileSize int
			var createdAt time.Time
			if err := attachRows.Scan(&att.ID, &att.Filename, &fileSize, &att.ContentType, &att.UploadedBy, &createdAt); err != nil {
				slog.Error("failed to scan attachment", "error", err)
				continue
			}
			att.FileSize = formatFileSize(fileSize)
			att.CreatedAt = createdAt.Format("Jan 2, 2006")
			att.DownloadURL = fmt.Sprintf("/rfps/%d/attachments/%d", id, att.ID)
			rfp.Attachments = append(rfp.Attachments, att)
		}
	}

	// Render template
	pageData := templates.PageData{
		Title:     rfp.Title,
		ActiveNav: "rfps",
		User: &templates.User{
			ID:        user.ID,
			Email:     user.Email,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			IsAdmin:   user.IsAdmin(),
		},
		Data: rfp,
	}

	if err := h.templates.Render(w, "rfp_detail", pageData); err != nil {
		slog.Error("failed to render RFP detail template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// Note represents a note on an RFP for display.
type Note struct {
	ID         int
	AuthorName string
	Content    string
	CreatedAt  string
}

// Attachment represents an uploaded file on an RFP for display.
type Attachment struct {
	ID           int
	Filename     string
	FileSize     string
	ContentType  string
	UploadedBy   string
	CreatedAt    string
	DownloadURL  string
}

// RFPDetailData contains data for the RFP detail template.
type RFPDetailData struct {
	ID             int
	Title          string
	Agency         string
	State          string
	City           string
	DueDate        string
	PostedDate     string
	Category       string
	VenueType      string
	SourceURL      string
	Portal         string
	PortalID       string
	TermMonths     int
	EstimatedValue float64
	Incumbent      string
	LoginRequired  bool
	LoginNotes     string
	DiscoveredAt   time.Time
	IsActive       bool

	// Tracking info
	Stage        string
	StageDisplay string
	Score        float64
	Priority     string

	// Notes
	Notes []Note

	// Attachments
	Attachments []Attachment
}

// formatFileSize formats a file size in bytes to a human-readable string.
func formatFileSize(bytes int) string {
	const (
		KB = 1024
		MB = KB * 1024
	)
	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
