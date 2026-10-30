// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

/// @title CLOBv1
/// @notice A spot central limit order book split into independent markets. Each market has a ladder
/// of price ticks 1 to 255, price-time FIFO queues, its own order ids and its own trading balances,
/// so calls to different markets read and write disjoint storage while calls to one market share its
/// book.
contract CLOBv1 {
    uint256 public constant LOW_PRICE = 90;
    uint256 public constant HIGH_PRICE = 100;
    uint256 public constant MAX_PRICE = 255;

    /// @notice The base and quote an account receives on its first call in a market.
    uint128 public constant CREDIT = 1e30;

    /// @notice A market reserved for gas estimation. The constructor leaves it one step before the
    /// cycle's taker that walks both levels, with each resting order from a different maker, and
    /// nothing is ever sent to it.
    uint256 public constant PROBE_MARKET = type(uint256).max;

    /// @notice Why batchUpdate rejected an order.
    uint8 public constant REJECT_POST_ONLY = 1;
    uint8 public constant REJECT_INVALID = 2;
    uint8 public constant REJECT_INSUFFICIENT_BALANCE = 3;
    uint8 public constant REJECT_DUPLICATE_CLOID = 4;

    /// @dev One resting order: two slots. Ids are local to their market.
    struct Order {
        address owner;
        uint64 size;
        bool isBuy;
        uint8 price;
        uint64 prev;
        uint64 next;
    }

    /// @dev A FIFO queue of resting orders at one price, as a doubly linked list.
    struct Queue {
        uint64 head;
        uint64 tail;
    }

    /// @dev bidTicks and askTicks have bit p set while the queue at price p is non-empty.
    struct Market {
        uint64 lastOrderId;
        uint64 round;
        uint256 bidTicks;
        uint256 askTicks;
        Queue[256] bids;
        Queue[256] asks;
    }

    struct Balance {
        uint128 baseAvailable;
        uint128 baseLocked;
        uint128 quoteAvailable;
        uint128 quoteLocked;
    }

    /// @dev The incoming order while it walks the book.
    struct Taker {
        address owner;
        uint256 market;
        uint64 id;
        bool isBuy;
        uint256 limit;
        uint256 remaining;
    }

    /// @dev One step of the order flow every market repeats.
    struct Step {
        bool isBuy;
        uint256 price;
        uint256 size;
    }

    /// @notice One order of a batchUpdate, named by the caller's client order id.
    struct NewOrder {
        uint64 cloid;
        bool isBuy;
        uint8 price;
        uint64 size;
        bool postOnly;
    }

    mapping(uint256 => Market) internal markets;
    mapping(uint256 => mapping(uint64 => Order)) internal orders;
    mapping(uint256 => mapping(address => Balance)) public balances;
    /// @notice The order id a client order id last rested as, per market and owner. It may name an
    /// order that has since been filled.
    mapping(uint256 => mapping(address => mapping(uint64 => uint64))) public orderIdOf;

    event OrderPlaced(
        uint256 indexed market,
        uint64 indexed orderId,
        address indexed owner,
        bool isBuy,
        uint256 price,
        uint256 filled,
        uint256 remaining
    );
    event Fill(
        uint256 indexed market,
        uint64 indexed takerOrderId,
        uint64 indexed makerOrderId,
        address maker,
        bool takerIsBuy,
        uint256 price,
        uint256 quantity,
        uint256 takerRemaining,
        uint256 makerRemaining
    );
    event OrderCancelled(uint256 indexed market, uint64 indexed orderId, address indexed owner);
    event OrderRejected(uint256 indexed market, address indexed owner, uint64 indexed cloid, uint8 reason);
    event CancelMissed(uint256 indexed market, address indexed owner, uint64 indexed cloid);

    error InvalidPrice();
    error InvalidSize();
    error InsufficientBalance();
    error NotOwner();

    constructor() {
        // Four makers rather than one, so the taker the probe prices loads four makers' balances.
        for (uint160 i = 1; i <= 4; i++) {
            _placeNext(address(i), PROBE_MARKET);
        }
    }

    /// @notice Credits the caller's available base and quote in a market.
    function credit(uint256 market, uint128 base, uint128 quote) external {
        Balance storage bal = balances[market][msg.sender];
        bal.baseAvailable += base;
        bal.quoteAvailable += quote;
    }

    /// @notice Places a limit order, matches it against the opposite side best price first, and
    /// rests any remainder at the tail of its price. Returns the order's market-local id.
    function place(uint256 market, bool isBuy, uint256 price, uint256 size) external returns (uint64) {
        return _place(msg.sender, market, isBuy, price, size);
    }

    /// @notice Places the next order of the market's repeating flow: two asks at the high price, two
    /// at the low price, a buy that walks both levels and fills three of them and part of the fourth,
    /// then a buy that empties the book. Market 0 is the caller's own market. The caller is credited
    /// on its first call in a market.
    function placeNext(uint256 market) external returns (uint64) {
        if (market == 0) {
            market = uint256(uint160(msg.sender));
        }
        return _placeNext(msg.sender, market);
    }

    /// @notice Cancels the caller's orders named by client order id, then places new orders, all in
    /// one market. Market 0 is the caller's own market, and the caller is credited on its first call in
    /// a market. A cancel that finds no resting order emits CancelMissed, and an order that cannot be
    /// placed emits OrderRejected; neither reverts the call.
    function batchUpdate(uint256 market, uint64[] calldata cancelCloids, NewOrder[] calldata newOrders) external {
        if (market == 0) {
            market = uint256(uint160(msg.sender));
        }
        _creditFirstCall(balances[market][msg.sender]);
        for (uint256 i = 0; i < cancelCloids.length; i++) {
            _cancelByCloid(market, cancelCloids[i]);
        }
        for (uint256 i = 0; i < newOrders.length; i++) {
            _placeByCloid(market, newOrders[i]);
        }
    }

    /// @notice Cancels the caller's resting order and releases its lock.
    function cancel(uint256 market, uint64 orderId) external {
        if (orders[market][orderId].owner != msg.sender) revert NotOwner();
        _cancel(market, orderId);
    }

    function getOrder(uint256 market, uint64 orderId) external view returns (Order memory) {
        return orders[market][orderId];
    }

    function getQueue(uint256 market, bool isBuy, uint256 price) external view returns (uint64 head, uint64 tail) {
        Market storage m = markets[market];
        uint8 p = _tick(price);
        Queue storage q = isBuy ? m.bids[p] : m.asks[p];
        return (q.head, q.tail);
    }

    /// @notice Returns the best bid and ask in a market, 0 for an empty side.
    function bestPrices(uint256 market) external view returns (uint256 bid, uint256 ask) {
        Market storage m = markets[market];
        if (m.bidTicks != 0) bid = _highestBit(m.bidTicks);
        if (m.askTicks != 0) ask = _lowestBit(m.askTicks);
    }

    function lastOrderId(uint256 market) external view returns (uint64) {
        return markets[market].lastOrderId;
    }

    function _placeNext(address owner, uint256 market) private returns (uint64) {
        _creditFirstCall(balances[market][owner]);
        Market storage m = markets[market];
        Step memory step = _step(m.round % 6);
        m.round += 1;
        return _place(owner, market, step.isBuy, step.price, step.size);
    }

    function _creditFirstCall(Balance storage bal) private {
        if (bal.baseAvailable == 0 && bal.baseLocked == 0 && bal.quoteAvailable == 0 && bal.quoteLocked == 0) {
            bal.baseAvailable = CREDIT;
            bal.quoteAvailable = CREDIT;
        }
    }

    function _step(uint256 index) private pure returns (Step memory) {
        if (index == 0) return Step(false, HIGH_PRICE, 3);
        if (index == 1) return Step(false, HIGH_PRICE, 3);
        if (index == 2) return Step(false, LOW_PRICE, 4);
        if (index == 3) return Step(false, LOW_PRICE, 4);
        if (index == 4) return Step(true, HIGH_PRICE, 12);
        return Step(true, HIGH_PRICE, 2);
    }

    function _place(address owner, uint256 market, bool isBuy, uint256 price, uint256 size)
        private
        returns (uint64)
    {
        uint8 p = _tick(price);
        if (size == 0 || size > type(uint64).max) revert InvalidSize();
        if (!_lock(balances[market][owner], isBuy, p, size)) revert InsufficientBalance();
        (uint64 id,) = _execute(owner, market, isBuy, p, size);
        return id;
    }

    /// @dev Places one batchUpdate order for msg.sender, rejecting rather than reverting.
    function _placeByCloid(uint256 market, NewOrder calldata o) private {
        if (o.price == 0 || o.size == 0) {
            emit OrderRejected(market, msg.sender, o.cloid, REJECT_INVALID);
            return;
        }
        mapping(uint64 => uint64) storage ids = orderIdOf[market][msg.sender];
        uint64 existing = ids[o.cloid];
        if (existing != 0 && orders[market][existing].owner != address(0)) {
            emit OrderRejected(market, msg.sender, o.cloid, REJECT_DUPLICATE_CLOID);
            return;
        }
        if (o.postOnly && _crosses(markets[market], o.isBuy, o.price)) {
            emit OrderRejected(market, msg.sender, o.cloid, REJECT_POST_ONLY);
            return;
        }
        if (!_lock(balances[market][msg.sender], o.isBuy, o.price, o.size)) {
            emit OrderRejected(market, msg.sender, o.cloid, REJECT_INSUFFICIENT_BALANCE);
            return;
        }
        (uint64 id, bool rested) = _execute(msg.sender, market, o.isBuy, o.price, o.size);
        if (rested) {
            ids[o.cloid] = id;
        }
    }

    /// @dev Cancels msg.sender's order named by cloid, or emits CancelMissed when none rests.
    function _cancelByCloid(uint256 market, uint64 cloid) private {
        mapping(uint64 => uint64) storage ids = orderIdOf[market][msg.sender];
        uint64 id = ids[cloid];
        if (id == 0) {
            emit CancelMissed(market, msg.sender, cloid);
            return;
        }
        delete ids[cloid];
        if (orders[market][id].owner == address(0)) {
            emit CancelMissed(market, msg.sender, cloid);
            return;
        }
        _cancel(market, id);
    }

    function _cancel(uint256 market, uint64 orderId) private {
        Order storage o = orders[market][orderId];
        address owner = o.owner;
        Balance storage bal = balances[market][owner];
        if (o.isBuy) {
            uint256 released = uint256(o.price) * o.size;
            bal.quoteLocked -= uint128(released);
            bal.quoteAvailable += uint128(released);
        } else {
            bal.baseLocked -= o.size;
            bal.baseAvailable += o.size;
        }
        Market storage m = markets[market];
        if (o.isBuy) {
            if (_unlink(market, m.bids[o.price], o.prev, o.next)) m.bidTicks &= ~(uint256(1) << o.price);
        } else {
            if (_unlink(market, m.asks[o.price], o.prev, o.next)) m.askTicks &= ~(uint256(1) << o.price);
        }
        delete orders[market][orderId];
        emit OrderCancelled(market, orderId, owner);
    }

    /// @dev Matches an order whose lock is already taken against the opposite side, best price first,
    /// and rests any remainder. Returns the order's id and whether any of it rested.
    function _execute(address owner, uint256 market, bool isBuy, uint8 price, uint256 size)
        private
        returns (uint64 id, bool rested)
    {
        Market storage m = markets[market];
        Taker memory t = Taker(owner, market, ++m.lastOrderId, isBuy, price, size);
        if (isBuy) {
            uint256 ticks = m.askTicks;
            uint256 walked = ticks;
            while (t.remaining > 0 && walked != 0) {
                uint8 p = _lowestBit(walked);
                if (p > price) break;
                if (!_match(t, m.asks[p], p)) break;
                walked &= ~(uint256(1) << p);
            }
            if (walked != ticks) m.askTicks = walked;
        } else {
            uint256 ticks = m.bidTicks;
            uint256 walked = ticks;
            while (t.remaining > 0 && walked != 0) {
                uint8 p = _highestBit(walked);
                if (p < price) break;
                if (!_match(t, m.bids[p], p)) break;
                walked &= ~(uint256(1) << p);
            }
            if (walked != ticks) m.bidTicks = walked;
        }
        rested = t.remaining > 0;
        if (rested) {
            _append(t, isBuy ? m.bids[price] : m.asks[price], price);
            uint256 bit = uint256(1) << price;
            if (isBuy) {
                if (m.bidTicks & bit == 0) m.bidTicks |= bit;
            } else {
                if (m.askTicks & bit == 0) m.askTicks |= bit;
            }
        }
        emit OrderPlaced(market, t.id, owner, isBuy, price, size - t.remaining, t.remaining);
        return (t.id, rested);
    }

    function _tick(uint256 price) private pure returns (uint8) {
        if (price == 0 || price > MAX_PRICE) revert InvalidPrice();
        return uint8(price);
    }

    /// @dev Reports whether an order at price would trade against the opposite side.
    function _crosses(Market storage m, bool isBuy, uint8 price) private view returns (bool) {
        if (isBuy) {
            uint256 asks = m.askTicks;
            return asks != 0 && _lowestBit(asks) <= price;
        }
        uint256 bids = m.bidTicks;
        return bids != 0 && _highestBit(bids) >= price;
    }

    /// @dev Moves the full order's base or quote from available to locked, or returns false when the
    /// available balance cannot cover it.
    function _lock(Balance storage bal, bool isBuy, uint256 price, uint256 size) private returns (bool) {
        if (isBuy) {
            uint256 amount = price * size;
            if (bal.quoteAvailable < amount) return false;
            bal.quoteAvailable -= uint128(amount);
            bal.quoteLocked += uint128(amount);
        } else {
            if (bal.baseAvailable < size) return false;
            bal.baseAvailable -= uint128(size);
            bal.baseLocked += uint128(size);
        }
        return true;
    }

    /// @dev Fills the taker against one price queue in time order, reducing t.remaining. Returns
    /// whether the queue is now empty.
    function _match(Taker memory t, Queue storage q, uint256 makerPrice) private returns (bool) {
        uint64 id = q.head;
        while (t.remaining > 0 && id != 0) {
            Order storage o = orders[t.market][id];
            uint256 qty = t.remaining < o.size ? t.remaining : o.size;
            t.remaining -= qty;
            uint256 makerLeft = o.size - qty;
            _settle(t, o.owner, makerPrice, qty);
            emit Fill(t.market, t.id, id, o.owner, t.isBuy, makerPrice, qty, t.remaining, makerLeft);
            if (makerLeft != 0) {
                o.size = uint64(makerLeft);
                return false;
            }
            uint64 next = o.next;
            delete orders[t.market][id];
            q.head = next;
            if (next == 0) {
                q.tail = 0;
            } else {
                orders[t.market][next].prev = 0;
            }
            id = next;
        }
        return id == 0;
    }

    /// @dev Moves one fill's base and quote between the taker and the maker.
    function _settle(Taker memory t, address makerOwner, uint256 makerPrice, uint256 qty) private {
        Balance storage taker = balances[t.market][t.owner];
        Balance storage maker = balances[t.market][makerOwner];
        uint256 notional = makerPrice * qty;
        if (t.isBuy) {
            taker.baseAvailable += uint128(qty);
            taker.quoteLocked -= uint128(t.limit * qty);
            taker.quoteAvailable += uint128(t.limit * qty - notional);
            maker.baseLocked -= uint128(qty);
            maker.quoteAvailable += uint128(notional);
        } else {
            taker.baseLocked -= uint128(qty);
            taker.quoteAvailable += uint128(notional);
            maker.baseAvailable += uint128(qty);
            maker.quoteLocked -= uint128(notional);
        }
    }

    /// @dev Rests the taker's remaining size at the tail of q.
    function _append(Taker memory t, Queue storage q, uint8 price) private {
        uint64 tail = q.tail;
        orders[t.market][t.id] =
            Order({owner: t.owner, size: uint64(t.remaining), isBuy: t.isBuy, price: price, prev: tail, next: 0});
        if (tail == 0) {
            q.head = t.id;
        } else {
            orders[t.market][tail].next = t.id;
        }
        q.tail = t.id;
    }

    /// @dev Removes an order from q. Returns whether q is now empty.
    function _unlink(uint256 market, Queue storage q, uint64 prev, uint64 next) private returns (bool) {
        if (prev == 0) {
            q.head = next;
        } else {
            orders[market][prev].next = next;
        }
        if (next == 0) {
            q.tail = prev;
        } else {
            orders[market][next].prev = prev;
        }
        return prev == 0 && next == 0;
    }

    /// @dev Returns the index of the lowest set bit of a non-zero x.
    function _lowestBit(uint256 x) private pure returns (uint8 r) {
        if (x & type(uint128).max == 0) {
            r += 128;
            x >>= 128;
        }
        if (x & type(uint64).max == 0) {
            r += 64;
            x >>= 64;
        }
        if (x & type(uint32).max == 0) {
            r += 32;
            x >>= 32;
        }
        if (x & type(uint16).max == 0) {
            r += 16;
            x >>= 16;
        }
        if (x & type(uint8).max == 0) {
            r += 8;
            x >>= 8;
        }
        if (x & 0xf == 0) {
            r += 4;
            x >>= 4;
        }
        if (x & 0x3 == 0) {
            r += 2;
            x >>= 2;
        }
        if (x & 0x1 == 0) {
            r += 1;
        }
    }

    /// @dev Returns the index of the highest set bit of a non-zero x.
    function _highestBit(uint256 x) private pure returns (uint8 r) {
        if (x >> 128 != 0) {
            r += 128;
            x >>= 128;
        }
        if (x >> 64 != 0) {
            r += 64;
            x >>= 64;
        }
        if (x >> 32 != 0) {
            r += 32;
            x >>= 32;
        }
        if (x >> 16 != 0) {
            r += 16;
            x >>= 16;
        }
        if (x >> 8 != 0) {
            r += 8;
            x >>= 8;
        }
        if (x >> 4 != 0) {
            r += 4;
            x >>= 4;
        }
        if (x >> 2 != 0) {
            r += 2;
            x >>= 2;
        }
        if (x >> 1 != 0) {
            r += 1;
        }
    }
}
