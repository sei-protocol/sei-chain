// A nested Go module boundary. The sei-db/db_engine/litt/ tree is a raw import
// from the upstream LittDB project and has not yet been adapted to this
// repo's dependency set (imports still reference github.com/Layr-Labs/...).
//
// Declaring this subtree as a separate module hides it from the parent
// module's `go mod tidy`, `go test ./...`, `go build ./...`, `go vet ./...`,
// and `golangci-lint run` — none of which cross module boundaries. See
// `sei-db/db_engine/litt/README.md` ("Work-in-progress guard") for the
// incremental integration policy.
//
// This file can be removed once the litt package fully compiles and passes lint.
module github.com/sei-protocol/sei-chain/sei-db/db_engine/litt

go 1.25.6
