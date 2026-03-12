package admin

import (
	"fmt"
	"net"

	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	"github.com/spf13/cast"
)

const (
	DefaultEnabled = false
	DefaultAddress = "127.0.0.1:9095"
)

// Config defines configuration for the admin gRPC server.
type Config struct {
	// Enabled controls whether the admin gRPC server starts.
	Enabled bool `mapstructure:"admin_enabled"`
	// Address is the listen address. Must be a loopback address.
	Address string `mapstructure:"admin_address"`
}

var DefaultConfig = Config{
	Enabled: DefaultEnabled,
	Address: DefaultAddress,
}

// ReadConfig reads admin config from app options (Viper-backed).
func ReadConfig(opts servertypes.AppOptions) (Config, error) {
	cfg := DefaultConfig
	if v := opts.Get("admin_server.admin_enabled"); v != nil {
		cfg.Enabled = cast.ToBool(v)
	}
	if v := opts.Get("admin_server.admin_address"); v != nil {
		if s := cast.ToString(v); s != "" {
			cfg.Address = s
		}
	}
	if cfg.Enabled {
		if err := validateLoopback(cfg.Address); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// validateLoopback ensures the address is bound to a loopback interface.
func validateLoopback(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid admin address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("admin address %q: host must be an IP, not a hostname", address)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("admin address %q: must be bound to loopback (127.0.0.1 or ::1), got %s", address, host)
	}
	return nil
}

// ConfigTemplate is the TOML template for the [admin] section of app.toml.
const ConfigTemplate = `
###############################################################################
###                       Admin Configuration (Auto-managed)                ###
###############################################################################
 
[admin_server]
 
# Enable the admin gRPC server for runtime log level control.
admin_enabled = {{ .Admin.Enabled }}
 
# Listen address for the admin gRPC server. Must be a loopback address.
admin_address = "{{ .Admin.Address }}"
`
