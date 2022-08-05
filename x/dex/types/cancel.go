package types

func (c *Cancellation) GetAccount() string {
	return c.Creator
}
