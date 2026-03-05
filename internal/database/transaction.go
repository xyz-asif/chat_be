package database

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// WithTransaction executes a function within a MongoDB transaction
// It handles session creation, transaction lifecycle, and error handling
func WithTransaction(ctx context.Context, client *mongo.Client, fn func(sessCtx context.Context) error) error {
	session, err := client.StartSession()
	if err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sessCtx context.Context) (interface{}, error) {
		if err := fn(sessCtx); err != nil {
			return nil, err
		}
		return nil, nil
	})

	return err
}

// TransactionFunc is a function type that can be executed within a transaction
type TransactionFunc func(ctx context.Context) error

// ExecuteInTransaction is a helper that wraps WithTransaction for cleaner usage
func ExecuteInTransaction(ctx context.Context, client *mongo.Client, fn TransactionFunc) error {
	return WithTransaction(ctx, client, fn)
}
