// Package services sits between the handlers and the repository.
package services

import (
	"context"
	"database/sql"

	"example.com/app/repo"
)

type Clients struct{ Repo *repo.Repo }

var Default = &Clients{Repo: &repo.Repo{}}

func (s *Clients) Get(ctx context.Context, id string) (*repo.Client, error) {
	return s.Repo.FindByID(ctx, id)
}

func (s *Clients) GetForCompany(ctx context.Context, companyID, id string) (*repo.Client, error) {
	return s.Repo.FindForCompany(ctx, companyID, id)
}

func (s *Clients) Search(ctx context.Context, term string) (*sql.Rows, error) {
	return s.Repo.SearchByName(ctx, term)
}
