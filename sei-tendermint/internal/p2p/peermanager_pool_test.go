package p2p

import (
)

// Test checking that TryStartDial respects MaxOut limit.
// - no more dials than len(out)+len(dialing)
// - permit is required for upgrade.
// - only upgrade dial is allowed when MaxOut is saturated.
// - no parallel upgrades allowed even if permit is provided.
// Test checking that pool manager behaves reasonably with MaxOut = 0
// Test checking that DialFailed WAI:
// - only dialed addresses are accepted
// Test checking that Connected behavior WAI:
// - for outbound only dialed addresses are accepted
// - for inbound the MaxIn is respected.
// - for inbound duplicates are accepted.
// - for outbound upgrade a low prio peer is disconnected and permit is cleared 
// Test checking connected/dialing addresses are not dialed.
// Test checking that disconnected/dial failed addresses are immediately available
//   for dialing again in case no other addresses are available.
// Test checking that TryStartDial does round robin in priority order
// - over all NodeIDs if there is <MaxOut outbound conns
// - over high priority NodeIDs for ==MaxOut outbound conns
// - populate the fixed addrs, bySender and extra (via public api of the poolManager).
// Test checking that interleaving PushPex and TryStartDial works as intended:
// - pushed addresses are immediately available.
// Test checking that inbound and outbound connection for the same NodeID can coexist.
// Test checking that InPool filter works as intended:
// - if PushPex/FixedAddrs inserts a mix of addresses form the pool and not from the pool,
//   filtered out entries should be never dialed.
// - inbound connections not from the pool should be rejected.
