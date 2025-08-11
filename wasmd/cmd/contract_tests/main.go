package main

import (
	"fmt"

	"github.com/snikch/goodman/hooks"
	"github.com/snikch/goodman/transaction"
)

func main() {
	// This must be compiled beforehand and given to dredd as parameter, in the meantime the server should be running
	h := hooks.NewHooks()
	server := hooks.NewServer(hooks.NewHooksRunner(h))
	h.BeforeAll(func(t []*transaction.Transaction) {
		fmt.Println("Sleep 5 seconds before all modification")
	})
	h.BeforeEach(func(t *transaction.Transaction) {
		fmt.Println("before each modification")
	})
	h.Before("/version > GET", func(t *transaction.Transaction) {
		fmt.Println("before version TEST")
	})
	h.Before("/node_version > GET", func(t *transaction.Transaction) {
		fmt.Println("before node_version TEST")
	})
	h.BeforeEachValidation(func(t *transaction.Transaction) {
		fmt.Println("before each validation modification")
	})
	h.BeforeValidation("/node_version > GET", func(t *transaction.Transaction) {
		fmt.Println("before validation node_version TEST")
	})
	h.After("/node_version > GET", func(t *transaction.Transaction) {
		fmt.Println("after node_version TEST")
	})
	h.AfterEach(func(t *transaction.Transaction) {
		fmt.Println("after each modification")
	})
	h.AfterAll(func(t []*transaction.Transaction) {
		fmt.Println("after all modification")
	})
	server.Serve()
	defer server.Listener.Close()
	fmt.Print(h)
}
