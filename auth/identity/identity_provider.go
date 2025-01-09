package identity

import "context"

type Provider interface {
	Upsert(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]string, error)
}
