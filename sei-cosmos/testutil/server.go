package testutil

type TestAppOpts struct{}

func (t TestAppOpts) Get(s string) interface{} {
	if s == "chain-id" {
		return "test-chain"
	}
	return nil
}
