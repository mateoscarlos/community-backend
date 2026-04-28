package feedback

import (
	"context"
	"database/sql"
	"fmt"

	feedbackdb "github.com/community-app/community-backend/internal/modules/feedback/repository/db"
)

type Repository interface {
	Create(ctx context.Context, message, contact string) (*Feedback, error)
	List(ctx context.Context, limit, offset int) ([]Feedback, error)
}

type postgresRepository struct {
	queries *feedbackdb.Queries
}

func NewPostgresRepository(sqlDB *sql.DB) Repository {
	return &postgresRepository{queries: feedbackdb.New(sqlDB)}
}

func (r *postgresRepository) Create(ctx context.Context, message, contact string) (*Feedback, error) {
	row, err := r.queries.CreateFeedback(ctx, feedbackdb.CreateFeedbackParams{
		Message: message,
		Contact: contact,
	})
	if err != nil {
		return nil, fmt.Errorf("create feedback: %w", err)
	}
	return toDomain(row), nil
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
