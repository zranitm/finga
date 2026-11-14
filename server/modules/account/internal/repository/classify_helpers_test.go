package repository

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func noRows() error { return pgx.ErrNoRows }

func constraint(name string) error {
	return &pgconn.PgError{Code: "23505", ConstraintName: name, Message: "duplicate key value violates unique constraint"}
}

func pgCode(code string) error { return &pgconn.PgError{Code: code} }
