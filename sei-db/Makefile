test-all: test-memiavl

test-memiavl:
	@cd sc/memiavl; go test -v -mod=readonly ./... -coverprofile=$(COVERAGE) -covermode=atomic