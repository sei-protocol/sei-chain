In this package is a PebbleDB-backed implementation of a block DB. The main goal of this implementation is to
(potentially) rule out PebbleDB as a viable backend for the block DB use case. In the past, I've observed pebble
and other LSM trees struggle with this type of workload, and so I sutrongly suspect that pebble will be ruled out here.
