package report

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

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
			columns: []string{"type", "name", "label", "code_id", "creator", "admin", "has_admin", "pointer"},
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

				// CSV order: name, type, label, code_id, creator, admin, has_admin, pointer
				// DB order:  type, name, label, code_id, creator, admin, has_admin, pointer
				return []interface{}{row[1], row[0], label, codeId, creator, admin, hasAdmin, pointer}, nil
			},
		},
	}

	for _, imp := range imports {
		if err := p.importTable(imp.table, imp.file, imp.columns, imp.resolver); err != nil {
			return fmt.Errorf("failed to import %s: %w", imp.table, err)
		}
		p.ctx.Logger().Info("CSV_IMPORT: Successfully imported table", "table", imp.table)
	}

	// Import account_asset with foreign key resolution
	return p.importAccountAsset()
}

func (p *PostgreSQLImporter) importTable(tableName, fileName string, columns []string, resolver func([]string) ([]interface{}, error)) error {
	filePath := fmt.Sprintf("%s/%s", p.outputDir, fileName)

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			p.ctx.Logger().Info("CSV_IMPORT: CSV file not found, skipping", "file", filePath)
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
			p.ctx.Logger().Info("CSV_IMPORT: Skipping invalid row", "error", err, "row", row)
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

	p.ctx.Logger().Info("CSV_IMPORT: Imported rows", "table", tableName, "count", rowCount)
	return nil
}

func (p *PostgreSQLImporter) importAccountAsset() error {
	p.ctx.Logger().Info("CSV_IMPORT: Starting importAccountAsset method")
	
	filePath := fmt.Sprintf("%s/account_asset.csv", p.outputDir)
	p.ctx.Logger().Info("CSV_IMPORT: Looking for account_asset file", "path", filePath)

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			p.ctx.Logger().Info("CSV_IMPORT: CSV file not found, skipping", "file", filePath)
			return nil
		}
		p.ctx.Logger().Error("CSV_IMPORT: Failed to open account_asset file", "error", err, "file", filePath)
		return fmt.Errorf("failed to open %s: %w", filePath, err)
	}
	defer file.Close()

	p.ctx.Logger().Info("CSV_IMPORT: Successfully opened account_asset file")

	reader := csv.NewReader(file)

	// Skip header
	header, err := reader.Read()
	if err != nil {
		p.ctx.Logger().Error("CSV_IMPORT: Failed to read header", "error", err)
		return fmt.Errorf("failed to read header: %w", err)
	}
	p.ctx.Logger().Info("CSV_IMPORT: Read header", "header", header)

	// Begin transaction
	p.ctx.Logger().Info("CSV_IMPORT: Beginning transaction")
	tx, err := p.db.Begin()
	if err != nil {
		p.ctx.Logger().Error("CSV_IMPORT: Failed to begin transaction", "error", err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Add diagnostic info
	var accountCount, assetCount int
	tx.QueryRow("SELECT COUNT(*) FROM account").Scan(&accountCount)
	tx.QueryRow("SELECT COUNT(*) FROM asset").Scan(&assetCount)
	p.ctx.Logger().Info("CSV_IMPORT: Starting account_asset import", "accounts_in_db", accountCount, "assets_in_db", assetCount)

	// Create table for bulk COPY
	p.ctx.Logger().Info("CSV_IMPORT: Creating table")
	tempTableName := fmt.Sprintf("temp_account_asset_%d", time.Now().Unix())
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			account_name TEXT,
			asset_name TEXT,
			balance TEXT,
			token_id TEXT
		)
	`, tempTableName)
	_, err = tx.Exec(createTableSQL)
	if err != nil {
		p.ctx.Logger().Error("CSV_IMPORT: Failed to create table", "error", err)
		return fmt.Errorf("failed to create table: %w", err)
	}
	p.ctx.Logger().Info("CSV_IMPORT: Table created successfully", "table", tempTableName)

	// Ensure cleanup of table
	defer func() {
		// Temporarily disable cleanup for debugging
		/*
		if _, err := p.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTableName)); err != nil {
			p.ctx.Logger().Error("CSV_IMPORT: Failed to drop table", "error", err, "table", tempTableName)
		} else {
			p.ctx.Logger().Info("CSV_IMPORT: Dropped table", "table", tempTableName)
		}
		*/
		p.ctx.Logger().Info("CSV_IMPORT: Keeping temp table for debugging", "table", tempTableName)
	}()

	// Use COPY for fast bulk import to table
	p.ctx.Logger().Info("CSV_IMPORT: Preparing COPY statement")
	stmt, err := tx.Prepare(pq.CopyIn(tempTableName, "account_name", "asset_name", "balance", "token_id"))
	if err != nil {
		p.ctx.Logger().Error("CSV_IMPORT: Failed to prepare COPY statement", "error", err)
		return fmt.Errorf("failed to prepare COPY statement: %w", err)
	}
	p.ctx.Logger().Info("CSV_IMPORT: COPY statement prepared successfully")

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
			p.ctx.Logger().Info("CSV_IMPORT: Skipping invalid account_asset row", "length", len(row))
			continue
		}

		if _, err := stmt.Exec(row[0], row[1], nullString(row[2]), nullString(row[3])); err != nil {
			return fmt.Errorf("failed to execute COPY: %w", err)
		}
		rowCount++
	}

	// Finalize COPY
	if _, err := stmt.Exec(); err != nil {
		return fmt.Errorf("failed to finalize COPY: %w", err)
	}
	if err := stmt.Close(); err != nil {
		return fmt.Errorf("failed to close COPY statement: %w", err)
	}

	p.ctx.Logger().Info("CSV_IMPORT: Bulk imported to table", "rows", rowCount)

	// Check table contents
	var tableRowCount int
	err = tx.QueryRow("SELECT COUNT(*) FROM "+tempTableName).Scan(&tableRowCount)
	if err != nil {
		return fmt.Errorf("failed to count table rows: %w", err)
	}
	p.ctx.Logger().Info("CSV_IMPORT: Table verification", "table_rows", tableRowCount)

	// Check for sample data in table
	var sampleAccount, sampleAsset string
	err = tx.QueryRow("SELECT account_name, asset_name FROM "+tempTableName+" LIMIT 1").Scan(&sampleAccount, &sampleAsset)
	if err != nil {
		p.ctx.Logger().Info("CSV_IMPORT: No sample data in table", "error", err.Error())
	} else {
		p.ctx.Logger().Info("CSV_IMPORT: Sample table data", "account", sampleAccount, "asset", sampleAsset)
	}

	// Check JOIN compatibility - see if we can find matching accounts/assets
	var matchingAccounts, matchingAssets int
	tx.QueryRow(`
		SELECT COUNT(DISTINCT t.account_name) 
		FROM `+tempTableName+` t 
		JOIN account a ON a.account = t.account_name
	`).Scan(&matchingAccounts)
	
	tx.QueryRow(`
		SELECT COUNT(DISTINCT t.asset_name) 
		FROM `+tempTableName+` t 
		JOIN asset ast ON ast.name = t.asset_name
	`).Scan(&matchingAssets)
	
	p.ctx.Logger().Info("CSV_IMPORT: JOIN compatibility check", "matching_accounts", matchingAccounts, "matching_assets", matchingAssets)

	// Now do efficient INSERT SELECT with JOINs for foreign key resolution
	p.ctx.Logger().Info("CSV_IMPORT: Starting INSERT SELECT operation")
	insertSQL := fmt.Sprintf(`
		INSERT INTO account_asset (account_id, asset_id, balance, token_id)
		SELECT a.account_id, ast.asset_id, 
			CASE WHEN t.balance = '' THEN NULL ELSE t.balance::NUMERIC END,
			COALESCE(t.token_id, '')
		FROM %s t
		JOIN account a ON a.account = t.account_name
		JOIN asset ast ON ast.name = t.asset_name
	`, tempTableName)
	
	p.ctx.Logger().Info("CSV_IMPORT: Executing INSERT SELECT", "sql", insertSQL)
	result, err := tx.Exec(insertSQL)
	if err != nil {
		p.ctx.Logger().Error("CSV_IMPORT: INSERT SELECT failed", "error", err, "sql", insertSQL)
		return fmt.Errorf("failed to insert from table: %w", err)
	}
	p.ctx.Logger().Info("CSV_IMPORT: INSERT SELECT completed successfully")

	insertedRows, _ := result.RowsAffected()
	p.ctx.Logger().Info("CSV_IMPORT: Inserted account_asset rows", "inserted", insertedRows, "table_rows", rowCount)

	if insertedRows < int64(rowCount) {
		p.ctx.Logger().Info("CSV_IMPORT: Some rows were skipped due to missing foreign keys", 
			"skipped", int64(rowCount)-insertedRows)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	p.ctx.Logger().Info("CSV_IMPORT: Successfully imported account_asset", "rows", insertedRows)
	return nil
}

func (p *PostgreSQLImporter) createSchemaIfNotExists() error {
	p.ctx.Logger().Info("CSV_IMPORT: Creating database schema if not exists")
	
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

	p.ctx.Logger().Info("CSV_IMPORT: Database schema created successfully")
	return nil
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
