package report

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/lib/pq"
)

// PostgreSQLImporter handles the import of CSV data to PostgreSQL
type PostgreSQLImporter struct {
	db        *sql.DB
	outputDir string
	ctx       sdk.Context
}

func NewPostgreSQLImporter(config PostgreSQLConfig, outputDir string, ctx sdk.Context) (*PostgreSQLImporter, error) {
	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host, config.Port, config.Username, config.Password, config.Database, config.SSLMode)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	return &PostgreSQLImporter{
		db:        db,
		outputDir: outputDir,
		ctx:       ctx,
	}, nil
}

func (p *PostgreSQLImporter) Close() error {
	return p.db.Close()
}

func (p *PostgreSQLImporter) ImportAll() error {
	// Create schema if it doesn't exist
	if err := p.createSchemaIfNotExists(); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Import in dependency order: accounts first, then assets, then account_asset
	imports := []struct {
		table    string
		file     string
		columns  []string
		resolver func([]string) ([]interface{}, error)
	}{
		{
			table:   "account",
			file:    "accounts.csv",
			columns: []string{"account", "evm_address", "evm_nonce", "sequence", "associated", "bucket"},
			resolver: func(row []string) ([]interface{}, error) {
				if len(row) != 6 {
					return nil, fmt.Errorf("invalid account row length: %d", len(row))
				}
				evmNonce, _ := strconv.Atoi(row[2])
				sequence, _ := strconv.Atoi(row[3])
				associated, _ := strconv.ParseBool(row[4])
				return []interface{}{row[0], row[1], evmNonce, sequence, associated, row[5]}, nil
			},
		},
		{
			table:   "asset",
			file:    "assets.csv",
			columns: []string{"name", "type", "label", "code_id", "creator", "admin", "has_admin", "pointer"},
			resolver: func(row []string) ([]interface{}, error) {
				if len(row) != 8 {
					return nil, fmt.Errorf("invalid asset row length: %d", len(row))
				}
				var codeId *int
				if row[3] != "" {
					if id, err := strconv.Atoi(row[3]); err == nil {
						codeId = &id
					}
				}
				hasAdmin, _ := strconv.ParseBool(row[6])

				// Handle empty values
				label := nullString(row[2])
				creator := nullString(row[4])
				admin := nullString(row[5])
				pointer := nullString(row[7])

				return []interface{}{row[1], row[0], label, codeId, creator, admin, hasAdmin, pointer}, nil
			},
		},
	}

	for _, imp := range imports {
		if err := p.importTable(imp.table, imp.file, imp.columns, imp.resolver); err != nil {
			return fmt.Errorf("failed to import %s: %w", imp.table, err)
		}
		p.ctx.Logger().Info("Successfully imported table", "table", imp.table)
	}

	// Import account_asset with foreign key resolution
	return p.importAccountAsset()
}

func (p *PostgreSQLImporter) importTable(tableName, fileName string, columns []string, resolver func([]string) ([]interface{}, error)) error {
	filePath := fmt.Sprintf("%s/%s", p.outputDir, fileName)

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			p.ctx.Logger().Info("CSV file not found, skipping", "file", filePath)
			return nil
		}
		return fmt.Errorf("failed to open %s: %w", filePath, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Skip header
	if _, err := reader.Read(); err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	// Begin transaction
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare COPY statement
	stmt, err := tx.Prepare(pq.CopyIn(tableName, columns...))
	if err != nil {
		return fmt.Errorf("failed to prepare COPY statement: %w", err)
	}

	rowCount := 0
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read CSV row: %w", err)
		}

		values, err := resolver(row)
		if err != nil {
			p.ctx.Logger().Info("Skipping invalid row", "error", err, "row", row)
			continue
		}

		if _, err := stmt.Exec(values...); err != nil {
			return fmt.Errorf("failed to execute COPY: %w", err)
		}
		rowCount++
	}

	if _, err := stmt.Exec(); err != nil {
		return fmt.Errorf("failed to finalize COPY: %w", err)
	}

	if err := stmt.Close(); err != nil {
		return fmt.Errorf("failed to close COPY statement: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	p.ctx.Logger().Info("Imported rows", "table", tableName, "count", rowCount)
	return nil
}

func (p *PostgreSQLImporter) importAccountAsset() error {
	filePath := fmt.Sprintf("%s/account_asset.csv", p.outputDir)

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			p.ctx.Logger().Info("CSV file not found, skipping", "file", filePath)
			return nil
		}
		return fmt.Errorf("failed to open %s: %w", filePath, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Skip header
	if _, err := reader.Read(); err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	// Begin transaction
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare COPY statement with foreign key resolution
	stmt, err := tx.Prepare(pq.CopyIn("account_asset", "account_id", "asset_id", "balance", "token_id"))
	if err != nil {
		return fmt.Errorf("failed to prepare COPY statement: %w", err)
	}

	rowCount := 0
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read CSV row: %w", err)
		}

		if len(row) != 4 {
			p.ctx.Logger().Info("Skipping invalid account_asset row", "length", len(row))
			continue
		}

		// Resolve account_id
		var accountID int
		err = tx.QueryRow("SELECT account_id FROM account WHERE account = $1", row[0]).Scan(&accountID)
		if err != nil {
			p.ctx.Logger().Info("Account not found, skipping", "account", row[0])
			continue
		}

		// Resolve asset_id
		var assetID int
		err = tx.QueryRow("SELECT asset_id FROM asset WHERE name = $1", row[1]).Scan(&assetID)
		if err != nil {
			p.ctx.Logger().Info("Asset not found, skipping", "asset", row[1])
			continue
		}

		balance := nullString(row[2])
		tokenID := nullString(row[3])

		if _, err := stmt.Exec(accountID, assetID, balance, tokenID); err != nil {
			return fmt.Errorf("failed to execute COPY: %w", err)
		}
		rowCount++
	}

	if _, err := stmt.Exec(); err != nil {
		return fmt.Errorf("failed to finalize COPY: %w", err)
	}

	if err := stmt.Close(); err != nil {
		return fmt.Errorf("failed to close COPY statement: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	p.ctx.Logger().Info("Imported account_asset rows", "count", rowCount)
	return nil
}

func (p *PostgreSQLImporter) createSchemaIfNotExists() error {
	p.ctx.Logger().Info("Creating database schema if not exists")
	
	// Create tables with the exact schema you provided
	schema := `
		-- Account table
		CREATE TABLE IF NOT EXISTS public.account (
			account_id  SERIAL PRIMARY KEY,
			account     TEXT NOT NULL UNIQUE,
			evm_address TEXT,
			evm_nonce   INTEGER,
			sequence    INTEGER,
			associated  BOOLEAN NOT NULL,
			bucket      TEXT NOT NULL
		);

		-- Asset table  
		CREATE TABLE IF NOT EXISTS public.asset (
			asset_id  SERIAL PRIMARY KEY,
			type      TEXT NOT NULL,
			name      TEXT UNIQUE,
			label     TEXT,
			code_id   INTEGER,
			creator   TEXT,
			admin     TEXT,
			has_admin BOOLEAN NOT NULL,
			pointer   TEXT,
			price     NUMERIC,
			decimals  INTEGER DEFAULT 1 NOT NULL
		);

		-- Account-Asset relationship table
		CREATE TABLE IF NOT EXISTS public.account_asset (
			account_id INTEGER NOT NULL REFERENCES public.account(account_id) ON DELETE CASCADE,
			asset_id   INTEGER NOT NULL REFERENCES public.asset(asset_id) ON DELETE CASCADE,
			balance    NUMERIC,
			token_id   TEXT DEFAULT ''::text NOT NULL,
			PRIMARY KEY (account_id, asset_id, token_id)
		);

		-- Create indexes for better performance
		CREATE INDEX IF NOT EXISTS idx_account_address ON public.account(account);
		CREATE INDEX IF NOT EXISTS idx_asset_name ON public.asset(name);
		CREATE INDEX IF NOT EXISTS idx_account_asset_account ON public.account_asset(account_id);
		CREATE INDEX IF NOT EXISTS idx_account_asset_asset ON public.account_asset(asset_id);
	`

	_, err := p.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	p.ctx.Logger().Info("Database schema created successfully")
	return nil
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
