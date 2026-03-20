package tcp

import (
	"context"
	"fmt"
	"net/netip"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestTestReservePort(t *testing.T) {
	for _, ip := range utils.Slice(
		IPv4Loopback(),
		netip.IPv4Unspecified(),
		netip.IPv6Loopback(),
		netip.IPv6Unspecified(),
	) {
		t.Run(ip.String(), func(t *testing.T) {
			addr := TestReservePort(ip)
			for i := range 5 {
				t.Logf("iteration %v - reserved port should be reopenable", i)
				err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
					t.Logf("open listener on %v", addr)
					l, err := Listen(addr)
					if err != nil {
						return fmt.Errorf("Listen(): %v", err)
					}
					t.Log("try to open duplicate listener (should fail)")
					if _, err = Listen(addr); err != nil {
						t.Logf("opening duplicate listener failed with: %v", err)
					} else {
						l.Close()
						return fmt.Errorf("duplicate listener created, while it should error")
					}
					t.Log("spawn task accepting connections")
					s.SpawnBg(func() error {
						for {
							conn, err := l.AcceptOrClose(ctx)
							if err != nil {
								return utils.IgnoreCancel(err)
							}
							defer conn.Close()
						}
					})
					t.Log("dial a bunch of connections")
					for range 3 {
						conn, err := Dial(ctx, addr)
						if err != nil {
							return fmt.Errorf("Dial(): %w", err)
						}
						defer conn.Close()
					}
					t.Log("connections established")
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
