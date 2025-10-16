# CSV Export and PostgreSQL Import for Sei Chain Reports

This package provides a comprehensive solution for exporting blockchain account and asset data directly to CSV files and importing them into PostgreSQL, eliminating the need for intermediate JSON processing.

## Features

- **Direct CSV Export**: Exports account, asset, and account_asset data directly to CSV format
- **Deduplication**: Built-in deduplication logic to prevent duplicate entries
- **PostgreSQL Integration**: Direct import to PostgreSQL using efficient COPY commands
- **Parallel Processing**: Concurrent export of different data types for improved performance
- **Foreign Key Resolution**: Automatic resolution of account and asset IDs during import

## Database Schema

The system exports data to three main tables:

```sql
-- Account table
CREATE TABLE public.account (
    account_id  SERIAL PRIMARY KEY,
    account     TEXT NOT NULL UNIQUE,
    evm_address TEXT,
    evm_nonce   INTEGER,
    sequence    INTEGER,
    associated  BOOLEAN NOT NULL,
    bucket      TEXT NOT NULL
);

-- Asset table  
CREATE TABLE public.asset (
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
CREATE TABLE public.account_asset (
    account_id INTEGER NOT NULL REFERENCES public.account ON DELETE CASCADE,
    asset_id   INTEGER NOT NULL REFERENCES public.asset ON DELETE CASCADE,
    balance    NUMERIC,
    token_id   TEXT DEFAULT ''::text NOT NULL,
    PRIMARY KEY (account_id, asset_id, token_id)
);
```

## Usage

### 1. Basic CSV Export

```go
import "github.com/sei-protocol/sei-chain/report"

// Create CSV service
csvService := report.NewCSVService(bankKeeper, accountKeeper, evmKeeper, wasmKeeper, "/tmp/export")

// Start export
ctx := sdk.NewContext(...)
err := csvService.Start(ctx)
if err != nil {
    log.Fatal("Export failed:", err)
}
```

### 2. Export with PostgreSQL Import

```go
// Create CSV service
csvService := report.NewCSVService(bankKeeper, accountKeeper, evmKeeper, wasmKeeper, "/tmp/export")

// Start export
ctx := sdk.NewContext(...)
err := csvService.Start(ctx)
if err != nil {
    log.Fatal("Export failed:", err)
}

// Configure PostgreSQL connection
config := report.PostgreSQLConfig{
    Host:     "localhost",
    Port:     5432,
    Database: "sei_data",
    Username: "postgres",
    Password: "password",
    SSLMode:  "disable",
}

// Import to PostgreSQL
err = csvService.ExportToPostgreSQL(config)
if err != nil {
    log.Fatal("PostgreSQL import failed:", err)
}
```

### 3. Using the RPC API

```bash
# Start CSV export
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "sei_startCSVReport",
    "params": ["/tmp/my-export"],
    "id": 1
  }'

# Check status
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "sei_reportStatus",
    "params": ["report-name"],
    "id": 2
  }'

# Export to PostgreSQL
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "sei_exportToPostgreSQL",
    "params": [
      "report-name",
      "localhost",
      5432,
      "sei_data",
      "postgres",
      "password",
      "disable"
    ],
    "id": 3
  }'
```

## Account Buckets

The system automatically categorizes accounts into buckets for analysis:

- `bonding_pool`: Staking bonding pool addresses
- `burn_address`: Token burn addresses  
- `sei_multisig`: Known Sei multisig addresses
- `multisig`: Other multisig accounts
- `gringotts`: Gringotts service addresses
- `cw_contract`: CosmWasm contract addresses
- `evm_contract`: EVM contract addresses
- `associated_evm`: EVM-associated accounts with transactions
- `associated_sei`: EVM-associated accounts without transactions
- `unassociated`: Non-EVM accounts
- `unknown`: Uncategorized accounts

## Data Types Exported

### Accounts (`accounts.csv`)
- Account address (Sei bech32)
- EVM address (hex)
- EVM nonce
- Sequence number
- Association status
- Account bucket classification

### Assets (`assets.csv`)
- Asset name/address
- Type (native, cw20, cw721)
- Label/display name
- Code ID (for contracts)
- Creator address
- Admin address
- Admin status
- Pointer address (if applicable)

### Account Assets (`account_asset.csv`)
- Account-asset relationships
- Balances for fungible tokens
- Token IDs for NFTs
- Automatic foreign key resolution during import

## Performance Considerations

- **Memory Efficient**: Streams data to CSV files without loading everything into memory
- **Concurrent Processing**: Exports accounts, assets, and relationships in parallel
- **Deduplication**: In-memory deduplication maps prevent duplicate entries
- **Batch Import**: Uses PostgreSQL COPY commands for efficient bulk import
- **Transaction Safety**: All imports are wrapped in transactions for consistency

## Error Handling

- Invalid CSV rows are logged and skipped
- Missing foreign key references are logged and skipped
- Database connection failures are properly reported
- Partial imports can be resumed by re-running the import process

## File Output

The system generates three CSV files:

1. `accounts.csv` - All blockchain accounts with metadata
2. `assets.csv` - All tokens and native assets  
3. `account_asset.csv` - Account-asset relationships and balances

These files are ready for PostgreSQL import using the `\copy` command or the built-in import functionality.
