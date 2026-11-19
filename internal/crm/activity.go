package crm

import (
	"context"
	"errors"
)

// ListActivity reads the timeline, newest first. list_recent_signups is
// ListActivity with Kind = ActivitySignedUp.
func (s *Service) ListActivity(ctx context.Context, q ActivityQuery) ([]Activity, error) {
	return nil, errors.New("not implemented")
}

// Status summarizes the instance for Settings and agents.
func (s *Service) Status(ctx context.Context) (Status, error) {
	return Status{}, errors.New("not implemented")
}
