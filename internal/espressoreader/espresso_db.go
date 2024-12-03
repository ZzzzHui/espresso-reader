// (c) Cartesi and individual authors (see AUTHORS)
// SPDX-License-Identifier: Apache-2.0 (see LICENSE)

package espressoreader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ZzzzHui/espresso-reader/internal/repository"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

func SetupEspressoDB(
	ctx context.Context,
	database *repository.Database) error {
	query := `CREATE TABLE IF NOT EXISTS "espresso_nonce"
(
    "sender_address" BYTEA NOT NULL,
    "application_address" BYTEA NOT NULL,
    "nonce" BIGINT NOT NULL,
    UNIQUE("sender_address", "application_address")
);`
	_, err := database.GetDB().Exec(ctx, query)
	if err != nil {
		slog.Error("failed to create table espresso_nonce")
		return err
	}

	query = `CREATE TABLE IF NOT EXISTS "espresso_block"
(
    "application_address" BYTEA PRIMARY KEY,
	"last_processed_espresso_block" NUMERIC(20,0) NOT NULL CHECK ("last_processed_espresso_block" >= 0 AND "last_processed_espresso_block" <= f_maxuint64())
);`
	_, err = database.GetDB().Exec(ctx, query)
	if err != nil {
		slog.Error("failed to create table espresso_block")
		return err
	}

	query = `CREATE TABLE IF NOT EXISTS "input_index"
(
    "application_address" BYTEA PRIMARY KEY,
	"index" BIGINT NOT NULL
);`
	_, err = database.GetDB().Exec(ctx, query)
	if err != nil {
		slog.Error("failed to create table input_index")
		return err
	}

	query = `ALTER TABLE input
	ADD COLUMN IF NOT EXISTS transaction_id BYTEA;`
	_, err = database.GetDB().Exec(ctx, query)
	if err != nil {
		slog.Error("failed to add column transaction_id to table input")
		return err
	}

	return nil
}

func GetLastProcessedEspressoBlock(
	ctx context.Context,
	database *repository.Database,
	application_address common.Address) (uint64, error) {

	var lastProcessedEspressoBlock uint64

	query := `
	SELECT
		last_processed_espresso_block
	FROM
		espresso_block
	WHERE
		application_address=@application_address`

	args := pgx.NamedArgs{
		"application_address": application_address,
	}

	err := database.GetDB().QueryRow(ctx, query, args).Scan(
		&lastProcessedEspressoBlock,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("GetLastProcessedEspressoBlock returned no rows",
				"app", lastProcessedEspressoBlock)
			return 0, nil
		}
		return 0, fmt.Errorf("GetLastProcessedEspressoBlock QueryRow failed: %w\n", err)
	}

	return lastProcessedEspressoBlock, nil
}

func UpdateLastProcessedEspressoBlock(
	ctx context.Context,
	database *repository.Database,
	application_address common.Address,
	last_processed_espresso_block uint64,
) error {

	query := `
	INSERT INTO espresso_block
		(application_address,
		last_processed_espresso_block)
	VALUES
		(@application_address,
		@last_processed_espresso_block)
	ON CONFLICT (application_address)
	DO UPDATE
		set last_processed_espresso_block=@last_processed_espresso_block
	`

	args := pgx.NamedArgs{
		"application_address":           application_address,
		"last_processed_espresso_block": last_processed_espresso_block,
	}
	_, err := database.GetDB().Exec(ctx, query, args)
	if err != nil {
		return fmt.Errorf("failed to update last_processed_espresso_block: %w", err)
	}

	return nil
}
