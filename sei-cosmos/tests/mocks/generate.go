package mocks

//go:generate go run github.com/golang/mock/mockgen@v1.7.0-rc.1 -source=../../client/account_retriever.go -package mocks -destination account_retriever.go
//go:generate go run github.com/golang/mock/mockgen@v1.7.0-rc.1 -source=../../types/module/module.go -package mocks -destination types_module_module.go
//go:generate go run github.com/golang/mock/mockgen@v1.7.0-rc.1 -source=../../types/invariant.go -package mocks -destination types_invariant.go
//go:generate go run github.com/golang/mock/mockgen@v1.7.0-rc.1 -source=../../types/router.go -package mocks -destination types_router.go
