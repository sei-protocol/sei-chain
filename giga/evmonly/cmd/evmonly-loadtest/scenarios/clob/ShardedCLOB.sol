// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

/// @title ShardedCLOB
/// @notice A spot central limit order book split into independent markets. Each market has two
/// configured price levels, price-time FIFO queues, its own order ids and its own trading balances,
/// so calls to different markets read and write disjoint storage.
contract ShardedCLOB {
    uint256 public constant LOW_PRICE = 90;
    uint256 public constant HIGH_PRICE = 100;

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
        Queue[2] bids;
        Queue[2] asks;
    }

    struct Balance {
        uint128 baseAvailable;
        uint128 baseLocked;
        uint128 quoteAvailable;
        uint128 quoteLocked;
    }

    mapping(uint256 => Market) internal markets; // slot 0
    mapping(uint256 => mapping(uint64 => Order)) internal orders; // slot 1
    mapping(uint256 => mapping(address => Balance)) public balances; // slot 2

    event OrderPlaced(
        uint256 indexed market,
        uint256 indexed orderId,
        address indexed owner,
        bool isBuy,
        uint256 price,
        uint256 filled,
        uint256 remaining
    );
    event Fill(
        uint256 indexed market,
        uint256 indexed takerOrderId,
        uint256 indexed makerOrderId,
        address taker,
        address maker,
        bool takerIsBuy,
        uint256 price,
        uint256 quantity,
        uint256 takerRemaining,
        uint256 makerRemaining
    );
    event OrderCancelled(uint256 indexed market, uint256 indexed orderId, address indexed owner, uint256 released);

    error InvalidPrice();
    error InvalidSize();
    error InsufficientBalance();
    error NotOwner();

    /// @notice Credits the caller's available base and quote in a market.
    function credit(uint256 market, uint128 base, uint128 quote) external {
        Balance storage bal = balances[market][msg.sender];
        bal.baseAvailable += base;
        bal.quoteAvailable += quote;
    }

    /// @notice Places a limit order, matches it against the opposite side best price first, and
    /// rests any remainder at the tail of its price. Returns the order's market-local id.
    function place(uint256 market, bool isBuy, uint256 price, uint256 size) external returns (uint64 orderId) {
        uint8 level = _level(price);
        if (size == 0 || size > type(uint64).max) revert InvalidSize();

        Balance storage taker = balances[market][msg.sender];
        _lock(taker, isBuy, price, size);

        Market storage m = markets[market];
        orderId = ++m.lastOrderId;

        uint256 remaining = size;
        if (isBuy) {
            remaining = _match(market, m.asks[0], LOW_PRICE, true, price, orderId, remaining, taker);
            if (price >= HIGH_PRICE) {
                remaining = _match(market, m.asks[1], HIGH_PRICE, true, price, orderId, remaining, taker);
            }
        } else {
            remaining = _match(market, m.bids[1], HIGH_PRICE, false, price, orderId, remaining, taker);
            if (price <= LOW_PRICE) {
                remaining = _match(market, m.bids[0], LOW_PRICE, false, price, orderId, remaining, taker);
            }
        }

        if (remaining > 0) {
            _append(market, isBuy ? m.bids[level] : m.asks[level], orderId, isBuy, level, remaining);
        }
        emit OrderPlaced(market, orderId, msg.sender, isBuy, price, size - remaining, remaining);
    }

    /// @notice Cancels the caller's resting order and releases its lock.
    function cancel(uint256 market, uint64 orderId) external {
        Order storage o = orders[market][orderId];
        if (o.owner != msg.sender) revert NotOwner();
        Market storage m = markets[market];
        Queue storage q = o.isBuy ? m.bids[o.level] : m.asks[o.level];
        uint256 size = o.size;
        Balance storage bal = balances[market][msg.sender];
        uint256 released;
        if (o.isBuy) {
            released = (o.level == 0 ? LOW_PRICE : HIGH_PRICE) * size;
            bal.quoteLocked -= uint128(released);
            bal.quoteAvailable += uint128(released);
        } else {
            released = size;
            bal.baseLocked -= uint128(size);
            bal.baseAvailable += uint128(size);
        }
        _unlink(market, q, o.prev, o.next);
        delete orders[market][orderId];
        emit OrderCancelled(market, orderId, msg.sender, released);
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

    function _level(uint256 price) private pure returns (uint8) {
        if (price == LOW_PRICE) return 0;
        if (price == HIGH_PRICE) return 1;
        revert InvalidPrice();
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

    /// @dev Fills the taker against one price queue in time order and returns the size left.
    function _match(
        uint256 market,
        Queue storage q,
        uint256 makerPrice,
        bool takerIsBuy,
        uint256 takerLimit,
        uint64 takerId,
        uint256 remaining,
        Balance storage taker
    ) private returns (uint256) {
        uint64 id = q.head;
        while (remaining > 0 && id != 0) {
            Order storage o = orders[market][id];
            uint256 makerSize = o.size;
            uint256 qty = remaining < makerSize ? remaining : makerSize;
            remaining -= qty;
            uint256 makerLeft = makerSize - qty;
            address makerOwner = o.owner;
            Balance storage maker = balances[market][makerOwner];
            uint256 notional = makerPrice * qty;
            if (takerIsBuy) {
                taker.baseAvailable += uint128(qty);
                taker.quoteLocked -= uint128(takerLimit * qty);
                taker.quoteAvailable += uint128(takerLimit * qty - notional);
                maker.baseLocked -= uint128(qty);
                maker.quoteAvailable += uint128(notional);
            } else {
                taker.baseLocked -= uint128(qty);
                taker.quoteAvailable += uint128(notional);
                maker.baseAvailable += uint128(qty);
                maker.quoteLocked -= uint128(notional);
            }
            emit Fill(market, takerId, id, msg.sender, makerOwner, takerIsBuy, makerPrice, qty, remaining, makerLeft);
            if (makerLeft != 0) {
                o.size = uint64(makerLeft);
                break;
            }
            uint64 next = o.next;
            delete orders[market][id];
            q.head = next;
            if (next == 0) {
                q.tail = 0;
            } else {
                orders[market][next].prev = 0;
            }
            id = next;
        }
        return remaining;
    }

    function _append(uint256 market, Queue storage q, uint64 id, bool isBuy, uint8 level, uint256 size) private {
        uint64 tail = q.tail;
        orders[market][id] =
            Order({owner: msg.sender, size: uint64(size), isBuy: isBuy, level: level, prev: tail, next: 0});
        if (tail == 0) {
            q.head = id;
        } else {
            orders[market][tail].next = id;
        }
        q.tail = id;
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
