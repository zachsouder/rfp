package scheduler

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/zachsouder/rfp/discovery/internal/search"
	"github.com/zachsouder/rfp/discovery/internal/validation"
	"github.com/zachsouder/rfp/shared/db"
	"github.com/zachsouder/rfp/shared/models"
)

// Store handles all database operations for the scheduler.
type Store struct {
	db *db.DB
}

// NewStore creates a new Store.
func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

// LoadQueryConfigs loads enabled query configs from the database.
// Falls back to defaults if none are found.
func (s *Store) LoadQueryConfigs(ctx context.Context) ([]models.SearchQueryConfig, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, query_template, enabled, created_at
		FROM discovery.search_query_configs
		WHERE enabled = true
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var configs []models.SearchQueryConfig
	for rows.Next() {
		var c models.SearchQueryConfig
		if err := rows.Scan(&c.ID, &c.Name, &c.QueryTemplate, &c.Enabled, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		configs = append(configs, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	// Fall back to defaults if no configs found
	if len(configs) == 0 {
		return search.DefaultQueryConfigs(), nil
	}

	return configs, nil
}

// SaveSearchQuery persists a search query execution record.
// Returns the query ID.
func (s *Store) SaveSearchQuery(ctx context.Context, queryText string, configID *int, resultsCount int, status string) (int, error) {
	var id int
	err := s.db.QueryRow(ctx, `
		INSERT INTO discovery.search_queries (query_text, query_config_id, results_count, status)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, queryText, configID, resultsCount, status).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert search query failed: %w", err)
	}
	return id, nil
}

// SaveSearchResult persists a search result.
// Returns the result ID.
func (s *Store) SaveSearchResult(ctx context.Context, queryID int, result search.Result) (int, error) {
	var id int
	err := s.db.QueryRow(ctx, `
		INSERT INTO discovery.search_results (query_id, url, title, snippet)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, queryID, result.URL, result.Title, result.Snippet).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert search result failed: %w", err)
	}
	return id, nil
}

// URLExists checks if a URL has already been processed.
func (s *Store) URLExists(ctx context.Context, url string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM discovery.search_results WHERE url = $1
		)
	`, url).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check url exists failed: %w", err)
	}
	return exists, nil
}

// URLExistsBatch checks multiple URLs at once and returns a map of URL -> exists.
func (s *Store) URLExistsBatch(ctx context.Context, urls []string) (map[string]bool, error) {
	if len(urls) == 0 {
		return make(map[string]bool), nil
	}

	rows, err := s.db.Query(ctx, `
		SELECT url FROM discovery.search_results WHERE url = ANY($1)
	`, urls)
	if err != nil {
		return nil, fmt.Errorf("batch url check failed: %w", err)
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, fmt.Errorf("scan url failed: %w", err)
		}
		result[url] = true
	}

	return result, rows.Err()
}

// UpdateValidation updates a search result with validation results.
func (s *Store) UpdateValidation(ctx context.Context, resultID int, valid bool, finalURL string, contentType string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE discovery.search_results
		SET url_validated = true, url_valid = $2, final_url = $3, content_type = $4
		WHERE id = $1
	`, resultID, valid, finalURL, contentType)
	if err != nil {
		return fmt.Errorf("update validation failed: %w", err)
	}
	return nil
}

// UpdateResearchStatus updates the research status of a search result.
func (s *Store) UpdateResearchStatus(ctx context.Context, resultID int, status string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE discovery.search_results
		SET research_status = $2
		WHERE id = $1
	`, resultID, status)
	if err != nil {
		return fmt.Errorf("update research status failed: %w", err)
	}
	return nil
}

// GetPendingResults returns search results that need research.
func (s *Store) GetPendingResults(ctx context.Context, limit int) ([]models.SearchResult, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, query_id, url, title, snippet, url_validated, url_valid, final_url, content_type,
		       hint_agency, hint_state, hint_due_date, research_status, promoted_rfp_id, duplicate_of_id, created_at
		FROM discovery.search_results
		WHERE research_status = 'pending' AND url_validated = true AND url_valid = true
		ORDER BY created_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending results failed: %w", err)
	}
	defer rows.Close()

	var results []models.SearchResult
	for rows.Next() {
		var r models.SearchResult
		if err := rows.Scan(
			&r.ID, &r.QueryID, &r.URL, &r.Title, &r.Snippet,
			&r.URLValidated, &r.URLValid, &r.FinalURL, &r.ContentType,
			&r.HintAgency, &r.HintState, &r.HintDueDate,
			&r.ResearchStatus, &r.PromotedRFPID, &r.DuplicateOfID, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan result failed: %w", err)
		}
		results = append(results, r)
	}

	return results, rows.Err()
}

// SaveSearchResultWithTx persists a search result within a transaction.
func (s *Store) SaveSearchResultWithTx(ctx context.Context, tx pgx.Tx, queryID int, result search.Result) (int, error) {
	var id int
	err := tx.QueryRow(ctx, `
		INSERT INTO discovery.search_results (query_id, url, title, snippet)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, queryID, result.URL, result.Title, result.Snippet).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert search result failed: %w", err)
	}
	return id, nil
}

// SearchResultWithID pairs a search result with its database ID.
type SearchResultWithID struct {
	ID     int
	URL    string
	Title  string
	Result search.Result
}

// SaveSearchQueryAndResults persists a query and its results in a transaction.
// Returns the query ID and a slice of result IDs.
func (s *Store) SaveSearchQueryAndResults(ctx context.Context, queryText string, configID *int, results []search.Result, status string) (int, []SearchResultWithID, error) {
	var queryID int
	var savedResults []SearchResultWithID

	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		// Save the query
		err := tx.QueryRow(ctx, `
			INSERT INTO discovery.search_queries (query_text, query_config_id, results_count, status)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, queryText, configID, len(results), status).Scan(&queryID)
		if err != nil {
			return fmt.Errorf("insert search query failed: %w", err)
		}

		// Save each result
		for _, r := range results {
			var resultID int
			err := tx.QueryRow(ctx, `
				INSERT INTO discovery.search_results (query_id, url, title, snippet)
				VALUES ($1, $2, $3, $4)
				RETURNING id
			`, queryID, r.URL, r.Title, r.Snippet).Scan(&resultID)
			if err != nil {
				return fmt.Errorf("insert search result failed: %w", err)
			}
			savedResults = append(savedResults, SearchResultWithID{
				ID:     resultID,
				URL:    r.URL,
				Title:  r.Title,
				Result: r,
			})
		}

		return nil
	})

	if err != nil {
		return 0, nil, err
	}

	return queryID, savedResults, nil
}

// UpdateValidationResult updates a search result with validation results using the validation package types.
func (s *Store) UpdateValidationResult(ctx context.Context, resultID int, vr *validation.Result) error {
	contentType := ""
	if vr.ContentType != "" {
		contentType = string(vr.ContentType)
	}

	_, err := s.db.Exec(ctx, `
		UPDATE discovery.search_results
		SET url_validated = true, url_valid = $2, final_url = $3, content_type = $4
		WHERE id = $1
	`, resultID, vr.Valid, vr.FinalURL, contentType)
	if err != nil {
		return fmt.Errorf("update validation failed: %w", err)
	}
	return nil
}
