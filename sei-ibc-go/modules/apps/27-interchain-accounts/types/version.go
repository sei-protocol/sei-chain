package types

import (
	"fmt"
	"regexp"
	"strings"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// DefaultMaxAddrLength defines the default maximum character length used in validation of addresses
var DefaultMaxAddrLength = 128

// IsValidAddr defines a regular expression to check if the provided string consists of
// strictly alphanumeric characters
var IsValidAddr = regexp.MustCompile("^[a-zA-Z0-9]*$").MatchString

// NewVersion returns a complete version string in the format: VersionPrefix + Delimter + AccAddress
func NewAppVersion(versionPrefix, accAddr string) string {
	return fmt.Sprint(versionPrefix, Delimiter, accAddr)
}

// ParseAddressFromVersion attempts to extract the associated account address from the provided version string
func ParseAddressFromVersion(version string) (string, error) {
	s := strings.Split(version, Delimiter)
	if len(s) != 2 {
		return "", sdkerrors.Wrap(ErrInvalidVersion, "failed to parse version")
	}

	return s[1], nil
}

// ValidateVersion performs basic validation of the provided ics27 version string.
// An ics27 version string may include an optional account address as per [TODO: Add spec when available]
// ValidateVersion first attempts to split the version string using the standard delimiter, then asserts a supported
// version prefix is included, followed by additional checks which enforce constraints on the account address.
func ValidateVersion(version string) error {
	s := strings.Split(version, Delimiter)

	if len(s) != 2 {
		return sdkerrors.Wrapf(ErrInvalidVersion, "expected format <app-version%saccount-address>, got %s", Delimiter, version)
	}

	if s[0] != VersionPrefix {
		return sdkerrors.Wrapf(ErrInvalidVersion, "expected %s, got %s", VersionPrefix, s[0])
	}

	if err := ValidateAccountAddress(s[1]); err != nil {
		return err
	}

	return nil
}

// ValidateAccountAddress performs basic validation of interchain account addresses, enforcing constraints
// on address length and character set
func ValidateAccountAddress(addr string) error {
	if !IsValidAddr(addr) || len(addr) == 0 || len(addr) > DefaultMaxAddrLength {
		return sdkerrors.Wrapf(
			ErrInvalidAccountAddress,
			"address must contain strictly alphanumeric characters, not exceeding %d characters in length",
			DefaultMaxAddrLength,
		)
	}

	return nil
}
