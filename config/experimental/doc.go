// Package experimental is the feature-flag framework for configuration values that change
// shape between binaries.
//
// A key declared here is outside the schema contract. It carries no fingerprint entry, needs
// no migration, and makes no compatibility promise across releases. An unrecognized key is
// reported and left in place rather than halting a boot, so a rollback does not lose it.
// Promoting a key to the stable registry is the commitment, and it changes the fingerprint.
//
// The acceptance gates in spec_test.go define this package's scope and are red until it is
// built. They sit behind the configspec build tag; removing the tag is what completes PR3.
package experimental
