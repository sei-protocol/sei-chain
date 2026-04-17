package admin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type mockAppOptions struct {
	values map[string]interface{}
}

func (m *mockAppOptions) Get(key string) interface{} {
	if m.values == nil {
		return nil
	}
	return m.values[key]
}

func TestDefaultConfig(t *testing.T) {
	require.False(t, DefaultConfig.Enabled)
	require.Equal(t, "127.0.0.1:9095", DefaultConfig.Address)
}

func TestReadConfig_Defaults(t *testing.T) {
	cfg, err := ReadConfig(&mockAppOptions{})
	require.NoError(t, err)
	require.Equal(t, DefaultEnabled, cfg.Enabled)
	require.Equal(t, DefaultAddress, cfg.Address)
}

func TestReadConfig_EnabledWithDefaultAddress(t *testing.T) {
	opts := &mockAppOptions{values: map[string]interface{}{
		"admin_server.admin_enabled": true,
	}}

	cfg, err := ReadConfig(opts)
	require.NoError(t, err)
	require.True(t, cfg.Enabled)
	require.Equal(t, DefaultAddress, cfg.Address)
}

func TestReadConfig_EnabledWithCustomLoopbackAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
	}{
		{"ipv4 loopback", "127.0.0.1:8080"},
		{"ipv4 loopback port 0", "127.0.0.1:0"},
		{"ipv6 loopback", "[::1]:9095"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &mockAppOptions{values: map[string]interface{}{
				"admin_server.admin_enabled": true,
				"admin_server.admin_address": tt.address,
			}}
			cfg, err := ReadConfig(opts)
			require.NoError(t, err)
			require.Equal(t, tt.address, cfg.Address)
		})
	}
}

func TestReadConfig_EnabledWithNonLoopbackAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
	}{
		{"public ipv4", "192.168.1.1:9095"},
		{"all interfaces", "0.0.0.0:9095"},
		{"public ipv6", "[2001:db8::1]:9095"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &mockAppOptions{values: map[string]interface{}{
				"admin_server.admin_enabled": true,
				"admin_server.admin_address": tt.address,
			}}
			_, err := ReadConfig(opts)
			require.Error(t, err, "non-loopback address %q should be rejected when enabled", tt.address)
		})
	}
}

func TestReadConfig_DisabledSkipsLoopbackValidation(t *testing.T) {
	opts := &mockAppOptions{values: map[string]interface{}{
		"admin_server.admin_enabled": false,
		"admin_server.admin_address": "0.0.0.0:9095",
	}}

	cfg, err := ReadConfig(opts)
	require.NoError(t, err, "disabled config should not validate address")
	require.False(t, cfg.Enabled)
	require.Equal(t, "0.0.0.0:9095", cfg.Address)
}

func TestReadConfig_EmptyAddressKeepsDefault(t *testing.T) {
	opts := &mockAppOptions{values: map[string]interface{}{
		"admin_server.admin_enabled": true,
		"admin_server.admin_address": "",
	}}

	cfg, err := ReadConfig(opts)
	require.NoError(t, err)
	require.Equal(t, DefaultAddress, cfg.Address)
}

func TestReadConfig_EnabledStringValue(t *testing.T) {
	opts := &mockAppOptions{values: map[string]interface{}{
		"admin_server.admin_enabled": "true",
	}}

	cfg, err := ReadConfig(opts)
	require.NoError(t, err)
	require.True(t, cfg.Enabled, "string 'true' should be cast to bool")
}

func TestValidateLoopback_ValidAddresses(t *testing.T) {
	tests := []struct {
		name    string
		address string
	}{
		{"ipv4 loopback", "127.0.0.1:9095"},
		{"ipv4 loopback port 0", "127.0.0.1:0"},
		{"ipv4 loopback high port", "127.0.0.1:65535"},
		{"ipv6 loopback", "[::1]:9095"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, validateLoopback(tt.address))
		})
	}
}

func TestValidateLoopback_InvalidAddresses(t *testing.T) {
	tests := []struct {
		name    string
		address string
		errSub  string
	}{
		{"no port", "127.0.0.1", "invalid admin address"},
		{"empty", "", "invalid admin address"},
		{"hostname", "localhost:9095", "host must be an IP"},
		{"public ipv4", "10.0.0.1:9095", "must be bound to loopback"},
		{"all interfaces", "0.0.0.0:9095", "must be bound to loopback"},
		{"public ipv6", "[2001:db8::1]:9095", "must be bound to loopback"},
		{"wildcard ipv6", "[::]:9095", "must be bound to loopback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLoopback(tt.address)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errSub)
		})
	}
}

func TestConfigTemplate_ContainsExpectedKeys(t *testing.T) {
	require.NotEmpty(t, ConfigTemplate)
	require.Contains(t, ConfigTemplate, "[admin_server]")
	require.Contains(t, ConfigTemplate, "admin_enabled")
	require.Contains(t, ConfigTemplate, "admin_address")
}
