# BUD tree shapes

This document shows how to draw a BUD tree for a given number of leaves, and is non-normative. BUD trees use the
Merkle tree defined in [RFC 9162 §2.1.1](https://www.rfc-editor.org/rfc/rfc9162#section-2.1.1); where this
document and the RFC disagree, the RFC is right. [`bud_spec.md`](bud_spec.md) defines what BUD adds to that tree,
including a different hash for the empty tree.

## Drawing rule

To draw the tree over `N` leaves:

1. Write `N` in binary. Each 1-bit is a perfect binary subtree of that many leaves. For example, 7 = 4 + 2 + 1.
2. Draw those subtrees side by side over the leaves, largest on the left.
3. Join from the right: put a parent over the two rightmost subtrees, then over that result and the next subtree
   to its left, and so on until one root remains.

The left side is always perfect, and all the irregularity hangs down the right edge. Nothing is ever duplicated:
a leftover node just sits on a longer edge. This is the same tree as the level-by-level definition in
`bud_spec.md` and as the RFC's split at the largest power of two below `N`. A tree over `N` leaves has `N − 1`
inner nodes.

## Examples

`L0 … L(N−1)` are the leaves in key order, `•` is an inner node, and `R` is the root. A vertical run is a node
carried up unpaired.

`N` = 1: the single leaf is the root.

```
L0
```

`N` = 2:

```
  R
 / \
L0  L1
```

`N` = 3 = 2 + 1:

```
     R
    / \
   /   \
  •     |
 / \    |
L0  L1  L2
```

`N` = 4, perfect:

```
      R
     / \
    /   \
   /     \
  •       •
 / \     / \
L0  L1  L2  L3
```

`N` = 5 = 4 + 1. `L4` is carried up to join directly under the root:

```
           R
          / \
         /   \
        /     \
       /       \
      •         |
     / \        |
    /   \       |
   /     \      |
  •       •     |
 / \     / \    |
L0  L1  L2  L3  L4
```

`N` = 6 = 4 + 2:

```
            R
           / \
          /   \
         /     \
        /       \
       /         \
      •           |
     / \          |
    /   \         |
   /     \        |
  •       •       •
 / \     / \     / \
L0  L1  L2  L3  L4  L5
```

`N` = 7 = 4 + (2 + 1):

```
             R
            / \
           /   \
          /     \
         /       \
        /         \
       /           \
      •             \
     / \             •
    /   \           / \
   /     \         /   \
  •       •       •     |
 / \     / \     / \    |
L0  L1  L2  L3  L4  L5  L6
```

`N` = 8, perfect:

```
              R
             / \
            /   \
           /     \
          /       \
         /         \
        /           \
       /             \
      •               •
     / \             / \
    /   \           /   \
   /     \         /     \
  •       •       •       •
 / \     / \     / \     / \
L0  L1  L2  L3  L4  L5  L6  L7
```

Skipping ahead to some larger trees:

`N` = 11 = 8 + (2 + 1):

```
                         R
                        / \
                       /   \
                      /     \
                     /       \
                    /         \
                   /           \
                  /             \
                 /               \
                /                 \
               /                   \
              •                     \
             / \                     |
            /   \                    |
           /     \                   |
          /       \                  |
         /         \                 |
        /           \                |
       /             \               |
      •               •              |
     / \             / \             •
    /   \           /   \           / \
   /     \         /     \         /   \
  •       •       •       •       •     |
 / \     / \     / \     / \     / \    |
L0  L1  L2  L3  L4  L5  L6  L7  L8  L9 L10
```

`N` = 13 = 8 + (4 + 1):

```
                            R
                           / \
                          /   \
                         /     \
                        /       \
                       /         \
                      /           \
                     /             \
                    /               \
                   /                 \
                  /                   \
                 /                     \
                /                       \
               /                         \
              •                           \
             / \                           |
            /   \                          |
           /     \                         •
          /       \                       / \
         /         \                     /   \
        /           \                   /     \
       /             \                 /       \
      •               •               •         |
     / \             / \             / \        |
    /   \           /   \           /   \       |
   /     \         /     \         /     \      |
  •       •       •       •       •       •     |
 / \     / \     / \     / \     / \     / \    |
L0  L1  L2  L3  L4  L5  L6  L7  L8  L9 L10 L11 L12
```

## Proofs

A BUD proof's `siblings` are the hashes of the nodes beside the path from a leaf to the root, leaf end first.

```
                            R
                           / \
                          /   \
                         /     \
                        /       \
                       /         \
                      /           \
                     /             \
                    /               \
                   /                 \
                  /                   \
                 /                     \
                /                       \
               /                         \
              I9                          \
             / \                           |
            /   \                          |
           /     \                        I10
          /       \                       / \
         /         \                     /   \
        /           \                   /     \
       /             \                 /       \
      I6              I7              I8        |
     / \             / \             / \        |
    /   \           /   \           /   \       |
   /     \         /     \         /     \      |
  I0      I1      I2      I3      I4      I5    |
 / \     / \     / \     / \     / \     / \    |
L0  L1  L2  L3  L4  L5  L6  L7  L8  L9 L10 L11 L12
```

```
siblings in the proof for L0:   leafHash(L1), I1, I7, I10
siblings in the proof for L9:   leafHash(L8), I5, leafHash(L12), I9
siblings in the proof for L12:  I8, I9
```
