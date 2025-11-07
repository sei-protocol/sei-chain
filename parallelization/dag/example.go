package dag

import (
	"context"
	"fmt"
	"log"
	"time"
)

// ExampleHandler is a simple transaction handler used by RunExample. It simulates work by
// sleeping for the specified duration and logs execution milestones.
func ExampleHandler(name string, duration time.Duration) TxHandler {
	return func(ctx context.Context, tx *TxContext) error {
		log.Printf("[%s] starting execution", name)
		select {
		case <-time.After(duration):
			log.Printf("[%s] finished execution", name)
			return nil
		case <-ctx.Done():
			log.Printf("[%s] cancelled", name)
			return ctx.Err()
		}
	}
}

// BankBalanceResource converts an account address into a bank balance resource identifier.
func BankBalanceResource(account string) ResourceID {
	return ResourceID(fmt.Sprintf("bank/balances/%s", account))
}

// ExampleTxBuilder constructs three dummy transactions with overlapping access patterns that
// exercise the DAG executor.
func ExampleTxBuilder() []*TxContext {
	tx1 := NewTxContext(0, "tx1_alice_to_bob", ExampleHandler("tx1", 40*time.Millisecond),
		NewAccessOp("tx1_read_alice", BankBalanceResource("alice"), AccessTypeRead),
		NewAccessOp("tx1_write_alice", BankBalanceResource("alice"), AccessTypeWrite),
		NewAccessOp("tx1_read_bob", BankBalanceResource("bob"), AccessTypeRead),
		NewAccessOp("tx1_write_bob", BankBalanceResource("bob"), AccessTypeWrite),
	)

	tx2 := NewTxContext(1, "tx2_bob_to_alice", ExampleHandler("tx2", 30*time.Millisecond),
		NewAccessOp("tx2_read_bob", BankBalanceResource("bob"), AccessTypeRead),
		NewAccessOp("tx2_write_bob", BankBalanceResource("bob"), AccessTypeWrite),
		NewAccessOp("tx2_read_alice", BankBalanceResource("alice"), AccessTypeRead),
		NewAccessOp("tx2_write_alice", BankBalanceResource("alice"), AccessTypeWrite),
	)

	tx3 := NewTxContext(2, "tx3_alice_to_bob", ExampleHandler("tx3", 25*time.Millisecond),
		NewAccessOp("tx3_read_alice", BankBalanceResource("alice"), AccessTypeRead),
		NewAccessOp("tx3_write_alice", BankBalanceResource("alice"), AccessTypeWrite),
		NewAccessOp("tx3_read_bob", BankBalanceResource("bob"), AccessTypeRead),
		NewAccessOp("tx3_write_bob", BankBalanceResource("bob"), AccessTypeWrite),
	)

	txs := []*TxContext{tx1, tx2, tx3}
	WireDependencies(txs)
	return txs
}

// RunExample executes the sample transactions with the DAG executor.
func RunExample() error {
	log.Println("starting parallel transaction DAG example")
	txs := ExampleTxBuilder()
	executor := NewDAGExecutor(txs)
	if err := executor.Execute(context.Background()); err != nil {
		return err
	}
	log.Println("completed parallel transaction DAG example")
	return nil
}
