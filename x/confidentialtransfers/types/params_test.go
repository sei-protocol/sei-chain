package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateEnableCtModule(t *testing.T) {
	t.Run("valid enable feature flag", func(t *testing.T) {
		flag := true
		err := validateEnableCtModule(flag)
		assert.Nil(t, err)
	})

	t.Run("invalid enable feature flag", func(t *testing.T) {
		flag := "True"
		err := validateEnableCtModule(flag)
		assert.Error(t, err)
	})
}

func TestValidateRangeProofGasMultiplier(t *testing.T) {
	t.Run("valid multiplier", func(t *testing.T) {
		multiplier := uint32(10)
		err := validateRangeProofGasMultiplier(multiplier)
		assert.Nil(t, err)
	})

	t.Run("valid but useless multiplier value", func(t *testing.T) {
		flag := uint32(1)
		err := validateRangeProofGasMultiplier(flag)
		assert.Nil(t, err)
	})

	t.Run("invalid multiplier value", func(t *testing.T) {
		flag := uint32(0)
		err := validateRangeProofGasMultiplier(flag)
		assert.Error(t, err)
	})

	t.Run("invalid multiplier type", func(t *testing.T) {
		flag := "True"
		err := validateRangeProofGasMultiplier(flag)
		assert.Error(t, err)
	})
}
