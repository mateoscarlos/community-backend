package feedback

import (
	"context"
	"database/sql"
	"fmt"

	feedbackdb "github.com/community-app/community-backend/internal/modules/feedback/repository/db"
)

type Repository interface {
	Create(ctx context.Context, message, contact string, rating *int, fbContext string) (*Feedback, error)
	List(ctx context.Context, limit, offset int) ([]Feedback, error)
}

type postgresRepository struct {
	db      *sql.DB
	queries *feedbackdb.Queries
}

func NewPostgresRepository(sqlDB *sql.DB) Repository {
	return &postgresRepository{db: sqlDB, queries: feedbackdb.New(sqlDB)}
}

// Create inserts a feedback row. rating/context are written via raw SQL (added
// after the sqlc-generated CreateFeedback) so the generated code stays the
// canonical source for the original message/contact shape.
func (r *postgresRepository) Create(ctx context.Context, message, contact string, rating *int, fbContext string) (*Feedback, error) {
	var (
		fb     Feedback
		dbRate sql.NullInt32
	)
	if rating != nil {
		dbRate = sql.NullInt32{Int32: int32(*rating), Valid: true}
	}
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO feedback (message, contact, rating, context)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, message, contact, rating, context, created_at`,
		message, contact, dbRate, fbContext,
	).Scan(&fb.ID, &fb.Message, &fb.Contact, &dbRate, &fb.Context, &fb.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create feedback: %w", err)
	}
	if dbRate.Valid {
		v := int(dbRate.Int32)
		fb.Rating = &v
	}
	return &fb, nil
}

func (r *postgresRepository) List(ctx context.Context, limit, offset int) ([]Feedback, error) {
	rows, err := r.queries.ListFeedback(ctx, feedbackdb.ListFeedbackParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	result := make([]Feedback, len(rows))
	for i, row := range rows {
		result[i] = *toDomain(row)
	}
	return result, nil
}

func toDomain(row feedbackdb.Feedback) *Feedback {
	return &Feedback{
		ID:        row.ID,
		Message:   row.Message,
		Contact:   row.Contact,
		CreatedAt: row.CreatedAt,
	}
}
