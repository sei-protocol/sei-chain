// Package walrus answers what value a key held at the end of a past block, without maintaining a
// multi-version store.
//
// Each block's changes are appended to a pod: a sequence of contiguous blocks held in one file, capped so
// that a uint32 addresses any byte of its data. Every pod carries a bloom filter and an index. A query walks
// pods backwards from the requested block, skipping any pod whose bloom filter rules the key out, and stops
// at the first pod holding a version of the key. A walk that finds nothing terminates at the newest retained
// snapshot at or below the requested block and reads the key from there.
//
// The name is an acronym: Write Ahead Log Reconstructing Unmaterialized State.
package walrus

/*       WALRUS: Write Ahead Log Reconstruction of Unmaterialized State
                                                                             :*%#+=+*#%=
                                                                          :#=           :%-
                                                                        :%.                -#
                                                                       *=   #*=   .---::-  .+%
                                                                      #:   :@%=      :   -  %@
                                                                     %: -.  :+.=   : +%:-+.   %:
                                                                    =-  -    +:  -.    :.   .: **
                                                                  :%%       =%= .=     .=    .:=#= =-
                                                                 *::+     *-@*: - :    :+    :  *@ #.
                                                                #: --   := *@=:  *=  :@#*@- :.  =@== #
                                                              +@+   =   . + @==+: +%@.    :@%:+:-%* + -
                                                            -% +       = * = +%#%-  %     .%  +%@* * +
                                                          -%-  *        = * *.+*+   %     **  .@@ + = -
                                                       .#+=-   =    :   . - +-.#:   @*+*#=.@   *%.. :
                                                     .%-  #     =   ..   . :.- #:   #      #.  *=-
                                                 .+%@*   -=      .   :-        %.  .*      =:  +.#
                                            :+%*-  +:    =:            #       %.  :-      .=  =-*:
                                       .*%*.      =      .-             -*     %.  =.       *  =--#
                                   =@#.          :.       .        .       *   %   *        *  =-.@-
                               :@#                                  *          %   %        #  =- *%
                            -@=                                      .*        %  :#        #  +. :==
                         .%+                                            *.     @  +-   ..   #  *    *
                       :%:                                                =:   @  #         #  *    #
                     :#.                                  =                 :  @  *         # .+    #
                    #:           .                       *              =.     % =:         # #.    #
                  ++             *                       =               .*    %:@          #.+    -#
                 #.             +.                 :    -:     -           *   .*           ==     %#.
               -%:-=.           *                  +    --   *.          +: @                     =-:#
             -%#:          .=   =                  =.    +  *             .*--                   -*  @
        .#%+         -.     ::   -  =               =    #.==               @+                - :*   *:
     +@=                #-   #       :              *.    %@     :           #     =        =- +*     #
  -@:   =*    .          .@  #       +:              +     @  ++             *=   *        %  @:      :@=
.@.  -%.   ++              +#.        -=              -.   %=%                #  #.      #- **            -%#
 :*#@.  .#:   :+          :* .+%#=.     *=                 =* :*#=.           #:*     .#-.#+             .   .*%+
     .::#*+=*=    +:     *+        .-+*#%%@%*-.           +@+:                #:  :+%@%*=-=*##+:            :+*- .%.
             -++=##***#*:                      .::--==#@*:                    %=-.              .-*##*+=-:::::-+%-.
                                                    **   .=-    -:           #:
                                                  #=   *=    =*     -=     .%:
                                                 %-.:#.   :#.    :#=     -%=
                                                     *%%%%%*+*#%@*=+*%%#:
*/

// Walrus is a historical state query engine over an append-only log of block changes.
type Walrus interface {

	// AppendBlock records the changes a block made.
	//
	// Blocks must arrive in contiguous ascending order: each block number exactly one greater than the last.
	// The first block appended to a fresh instance may be any number and sets the baseline.
	//
	// The block is gathered into the pod being accumulated and is not queryable until that pod is written,
	// which happens once a later block would overflow it.
	AppendBlock(block Block) error

	// Flush writes the pod being accumulated, short of its size limit or not, making every block appended so
	// far durable and queryable.
	Flush() error

	// RetainSnapshot takes an independent reference to a state snapshot, which this instance then reads to
	// terminate backwards walks.
	//
	// directory must hold an immutable flat image of the entire state as of the end of blockNumber. Every
	// file under it is hard-linked into this instance's own tree, so the caller keeps its directory and may
	// delete it as soon as this returns: both names point at the same inodes, and the data survives until the
	// last one is unlinked. That is what lets this instance's retention window and whatever pruner the
	// producer runs operate without knowing about each other.
	//
	// Immutability is the precondition rather than a nicety. A hard link shares the inode, so a producer that
	// rewrote a file in place would rewrite it underneath a running query. A checkpoint satisfies this; a
	// live database directory does not.
	//
	// Hardlinks cannot cross filesystems, so directory must live on the same one as the configured path.
	//
	// Snapshots may be retained at any block heights, in any order relative to appends, and need not align
	// with pod boundaries.
	RetainSnapshot(blockNumber uint64, directory string) error

	// Get returns the value key held at the end of blockNumber.
	//
	// status distinguishes a key that held no value from a block this instance cannot answer for; value is
	// nil unless status is ReadFound. A zero-length value that was actually written is returned as a non-nil
	// empty slice, so that an empty value is distinguishable from an absent key.
	Get(key []byte, blockNumber uint64) (value []byte, status ReadStatus, err error)

	// QueryableBounds reports the range of blocks Get can answer, which spans the oldest retained pod through
	// the newest written one. Blocks still being accumulated are excluded.
	QueryableBounds() (
		// If true, at least one pod is retained and first/last are valid. If false, the instance has
		// nothing to query and first/last are undefined.
		ok bool,
		// The lowest queryable block number, inclusive. Only valid if ok is true.
		first uint64,
		// The highest queryable block number, inclusive. Only valid if ok is true.
		last uint64,
		// Any error encountered while determining the range.
		err error,
	)

	// Close writes the pod being accumulated and releases resources. That pod is built like any other, so
	// every appended block is queryable after the next open.
	Close() error
}
