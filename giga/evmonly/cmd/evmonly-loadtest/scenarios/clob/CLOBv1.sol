// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

/// @title CLOBv1
/// @notice A spot central limit order book split into independent markets. Each market has two
/// price levels, price-time FIFO queues, its own order ids and its own trading balances, so calls to
/// different markets read and write disjoint storage.
contract CLOBv1 {
    uint256 public constant LOW_PRICE = 90;
    uint256 public constant HIGH_PRICE = 100;

    /// @notice The base and quote an account receives on its first call in a market.
    uint128 public constant CREDIT = 1e30;

    /// @notice A market reserved for gas estimation. The constructor leaves it one step before the
    /// cycle's taker that walks both levels, with each resting order from a different maker, and
    /// nothing is ever sent to it.
    uint256 public constant PROBE_MARKET = type(uint256).max;

    /// @dev One resting order: two slots. Ids are local to their market.
    struct Order {
        address owner;
        uint64 size;
        bool isBuy;
        uint8 level;
        uint64 prev;
        uint64 next;
    }

    /// @dev A FIFO queue of resting orders at one price, as a doubly linked list.
    struct Queue {
        uint64 head;
        uint64 tail;
    }

    struct Market {
        uint64 lastOrderId;
        uint64 round;
        Queue[2] bids;
        Queue[2] asks;
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

    mapping(uint256 => Market) internal markets;
    mapping(uint256 => mapping(uint64 => Order)) internal orders;
    mapping(uint256 => mapping(address => Balance)) public balances;

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

    /// @notice Cancels the caller's resting order and releases its lock.
    function cancel(uint256 market, uint64 orderId) external {
        Order storage o = orders[market][orderId];
        if (o.owner != msg.sender) revert NotOwner();
        Balance storage bal = balances[market][msg.sender];
        if (o.isBuy) {
            uint256 released = _price(o.level) * o.size;
            bal.quoteLocked -= uint128(released);
            bal.quoteAvailable += uint128(released);
        } else {
            bal.baseLocked -= o.size;
            bal.baseAvailable += o.size;
        }
        Market storage m = markets[market];
        _unlink(market, o.isBuy ? m.bids[o.level] : m.asks[o.level], o.prev, o.next);
        delete orders[market][orderId];
        emit OrderCancelled(market, orderId, msg.sender);
    }

    function getOrder(uint256 market, uint64 orderId) external view returns (Order memory) {
        return orders[market][orderId];
    }

    function getQueue(uint256 market, bool isBuy, uint256 price) external view returns (uint64 head, uint64 tail) {
        Market storage m = markets[market];
        Queue storage q = isBuy ? m.bids[_level(price)] : m.asks[_level(price)];
        return (q.head, q.tail);
    }

    function lastOrderId(uint256 market) external view returns (uint64) {
        return markets[market].lastOrderId;
    }

    function _placeNext(address owner, uint256 market) private returns (uint64) {
        Balance storage bal = balances[market][owner];
        if (bal.baseAvailable == 0 && bal.baseLocked == 0 && bal.quoteAvailable == 0 && bal.quoteLocked == 0) {
            bal.baseAvailable = CREDIT;
            bal.quoteAvailable = CREDIT;
        }
        Market storage m = markets[market];
        Step memory step = _step(m.round % 6);
        m.round += 1;
        return _place(owner, market, step.isBuy, step.price, step.size);
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
        uint8 level = _level(price);
        if (size == 0 || size > type(uint64).max) revert InvalidSize();
        _lock(balances[market][owner], isBuy, price, size);

        Market storage m = markets[market];
        Taker memory t = Taker(owner, market, ++m.lastOrderId, isBuy, price, size);
        if (isBuy) {
            _match(t, m.asks[0], LOW_PRICE);
            if (price >= HIGH_PRICE) _match(t, m.asks[1], HIGH_PRICE);
        } else {
            _match(t, m.bids[1], HIGH_PRICE);
            if (price <= LOW_PRICE) _match(t, m.bids[0], LOW_PRICE);
        }
        if (t.remaining > 0) {
            _append(t, isBuy ? m.bids[level] : m.asks[level], level);
        }
        emit OrderPlaced(market, t.id, owner, isBuy, price, size - t.remaining, t.remaining);
        return t.id;
    }

    function _level(uint256 price) private pure returns (uint8) {
        if (price == LOW_PRICE) return 0;
        if (price == HIGH_PRICE) return 1;
        revert InvalidPrice();
    }

    function _price(uint8 level) private pure returns (uint256) {
        return level == 0 ? LOW_PRICE : HIGH_PRICE;
    }

    function _lock(Balance storage bal, bool isBuy, uint256 price, uint256 size) private {
        if (isBuy) {
            uint256 amount = price * size;
            if (bal.quoteAvailable < amount) revert InsufficientBalance();
            bal.quoteAvailable -= uint128(amount);
            bal.quoteLocked += uint128(amount);
        } else {
            if (bal.baseAvailable < size) revert InsufficientBalance();
            bal.baseAvailable -= uint128(size);
            bal.baseLocked += uint128(size);
        }
    }

    /// @dev Fills the taker against one price queue in time order, reducing t.remaining.
    function _match(Taker memory t, Queue storage q, uint256 makerPrice) private {
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
                return;
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
    function _append(Taker memory t, Queue storage q, uint8 level) private {
        uint64 tail = q.tail;
        orders[t.market][t.id] =
            Order({owner: t.owner, size: uint64(t.remaining), isBuy: t.isBuy, level: level, prev: tail, next: 0});
        if (tail == 0) {
            q.head = t.id;
        } else {
            orders[t.market][tail].next = t.id;
        }
        q.tail = t.id;
    }

    function _unlink(uint256 market, Queue storage q, uint64 prev, uint64 next) private {
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
    }
}
