package rpcadmission

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/spf13/cast"
)

const SectionName = "rpc_admission"

const (
	flagGlobalLimit   = SectionName + ".global_limit"
	classLimitsPath   = SectionName + ".class_limits."
	classTimeoutsPath = SectionName + ".class_timeouts."
)

// AppOptions is the flat key-value view used to read the admission section.
type AppOptions interface {
	Get(string) interface{}
}

// ClassLimits sets the maximum admission weight held by each method class.
// A zero limit disables the corresponding class-specific semaphore.
type ClassLimits struct {
	CheapRead              int64 `mapstructure:"cheap_read"`
	NormalRead             int64 `mapstructure:"normal_read"`
	SearchIndex            int64 `mapstructure:"search_index"`
	BlockTxMaterialization int64 `mapstructure:"block_tx_materialization"`
	LogQuery               int64 `mapstructure:"log_query"`
	EVMExecution           int64 `mapstructure:"evm_execution"`
	Trace                  int64 `mapstructure:"trace"`
	Broadcast              int64 `mapstructure:"broadcast"`
	Subscription           int64 `mapstructure:"subscription"`
}

// ClassTimeouts sets how long admission may wait for capacity in each method class.
// A zero timeout attempts admission without waiting.
type ClassTimeouts struct {
	CheapRead              time.Duration `mapstructure:"cheap_read"`
	NormalRead             time.Duration `mapstructure:"normal_read"`
	SearchIndex            time.Duration `mapstructure:"search_index"`
	BlockTxMaterialization time.Duration `mapstructure:"block_tx_materialization"`
	LogQuery               time.Duration `mapstructure:"log_query"`
	EVMExecution           time.Duration `mapstructure:"evm_execution"`
	Trace                  time.Duration `mapstructure:"trace"`
	Broadcast              time.Duration `mapstructure:"broadcast"`
	Subscription           time.Duration `mapstructure:"subscription"`
}

// Config configures the node-wide weighted RPC admission pool.
type Config struct {
	// GlobalLimit is the total admission weight that may be held across all RPC planes.
	// Zero disables the global semaphore while leaving configured class limits active.
	GlobalLimit   int64         `mapstructure:"global_limit"`
	ClassLimits   ClassLimits   `mapstructure:"class_limits"`
	ClassTimeouts ClassTimeouts `mapstructure:"class_timeouts"`
}

// DefaultConfig is the admission configuration written for a new node.
var DefaultConfig = Config{
	GlobalLimit: 100,
	ClassLimits: ClassLimits{
		CheapRead:              100,
		NormalRead:             50,
		SearchIndex:            16,
		BlockTxMaterialization: 16,
		LogQuery:               20,
		EVMExecution:           20,
		Trace:                  20,
		Broadcast:              50,
		Subscription:           100,
	},
	ClassTimeouts: ClassTimeouts{
		CheapRead:              time.Second,
		NormalRead:             time.Second,
		SearchIndex:            2 * time.Second,
		BlockTxMaterialization: 2 * time.Second,
		LogQuery:               2 * time.Second,
		EVMExecution:           2 * time.Second,
		Trace:                  time.Second,
		Broadcast:              time.Second,
		Subscription:           time.Second,
	},
}

func init() {
	registry.RegisterSection(SectionName, &Config{}, defaults)
}

func defaults(registry.Mode) any { return DefaultConfig }

// ReadConfig reads the rpc_admission section from app options.
func ReadConfig(opts AppOptions) (Config, error) {
	cfg := DefaultConfig
	if err := readInt64(opts, flagGlobalLimit, &cfg.GlobalLimit); err != nil {
		return Config{}, err
	}
	for _, setting := range limitSettings(&cfg.ClassLimits) {
		if err := readInt64(opts, classLimitsPath+string(setting.class), setting.value); err != nil {
			return Config{}, err
		}
	}
	for _, setting := range timeoutSettings(&cfg.ClassTimeouts) {
		if err := readDuration(opts, classTimeoutsPath+string(setting.class), setting.value); err != nil {
			return Config{}, err
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func readInt64(opts AppOptions, key string, target *int64) error {
	value := opts.Get(key)
	if value == nil {
		return nil
	}
	parsed, err := cast.ToInt64E(value)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	*target = parsed
	return nil
}

func readDuration(opts AppOptions, key string, target *time.Duration) error {
	value := opts.Get(key)
	if value == nil {
		return nil
	}
	parsed, err := cast.ToDurationE(value)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	*target = parsed
	return nil
}

// Validate checks that every admission limit and timeout is usable.
func (c Config) Validate() error {
	if c.GlobalLimit < 0 {
		return fmt.Errorf("%s must be >= 0", flagGlobalLimit)
	}
	maxWeight := maxClassWeight()
	if c.GlobalLimit > 0 && c.GlobalLimit < maxWeight {
		return fmt.Errorf("%s must be 0 or at least the maximum single-method weight %d", flagGlobalLimit, maxWeight)
	}
	for _, setting := range limitSettings(&c.ClassLimits) {
		if *setting.value < 0 {
			return fmt.Errorf("%s%s must be >= 0", classLimitsPath, setting.class)
		}
		if *setting.value > 0 && *setting.value < setting.class.Weight() {
			return fmt.Errorf("%s%s must be 0 or at least the class weight %d", classLimitsPath, setting.class, setting.class.Weight())
		}
	}
	for _, setting := range timeoutSettings(&c.ClassTimeouts) {
		if *setting.value < 0 {
			return fmt.Errorf("%s%s must be >= 0", classTimeoutsPath, setting.class)
		}
	}
	return nil
}

func maxClassWeight() int64 {
	var maxWeight int64
	for _, class := range allMethodClasses {
		if weight := class.Weight(); weight > maxWeight {
			maxWeight = weight
		}
	}
	return maxWeight
}

type limitSetting struct {
	class MethodClass
	value *int64
}

func limitSettings(limits *ClassLimits) []limitSetting {
	return []limitSetting{
		{ClassCheapRead, &limits.CheapRead},
		{ClassNormalRead, &limits.NormalRead},
		{ClassSearchIndex, &limits.SearchIndex},
		{ClassBlockTxMaterialization, &limits.BlockTxMaterialization},
		{ClassLogQuery, &limits.LogQuery},
		{ClassEVMExecution, &limits.EVMExecution},
		{ClassTrace, &limits.Trace},
		{ClassBroadcast, &limits.Broadcast},
		{ClassSubscription, &limits.Subscription},
	}
}

type timeoutSetting struct {
	class MethodClass
	value *time.Duration
}

func timeoutSettings(timeouts *ClassTimeouts) []timeoutSetting {
	return []timeoutSetting{
		{ClassCheapRead, &timeouts.CheapRead},
		{ClassNormalRead, &timeouts.NormalRead},
		{ClassSearchIndex, &timeouts.SearchIndex},
		{ClassBlockTxMaterialization, &timeouts.BlockTxMaterialization},
		{ClassLogQuery, &timeouts.LogQuery},
		{ClassEVMExecution, &timeouts.EVMExecution},
		{ClassTrace, &timeouts.Trace},
		{ClassBroadcast, &timeouts.Broadcast},
		{ClassSubscription, &timeouts.Subscription},
	}
}

func (l ClassLimits) limit(class MethodClass) int64 {
	for _, setting := range limitSettings(&l) {
		if setting.class == class {
			return *setting.value
		}
	}
	return l.NormalRead
}

func (t ClassTimeouts) timeout(class MethodClass) time.Duration {
	for _, setting := range timeoutSettings(&t) {
		if setting.class == class {
			return *setting.value
		}
	}
	return t.NormalRead
}

// ConfigTemplate is the TOML template for the rpc_admission section of app.toml.
const ConfigTemplate = `
###############################################################################
###                    RPC Admission Configuration                          ###
###############################################################################

[rpc_admission]

# Total weighted capacity shared by EVM, CometBFT, gRPC, and gRPC-Web RPCs.
# Zero disables only the global limit.
global_limit = {{ .RPCAdmission.GlobalLimit }}

[rpc_admission.class_limits]
# Maximum weighted capacity held by each class. Zero disables that class limit.
# One request consumes the weight shown beside its class.
# weight: 1
cheap_read = {{ .RPCAdmission.ClassLimits.CheapRead }}
# weight: 2
normal_read = {{ .RPCAdmission.ClassLimits.NormalRead }}
# weight: 8
search_index = {{ .RPCAdmission.ClassLimits.SearchIndex }}
# weight: 8
block_tx_materialization = {{ .RPCAdmission.ClassLimits.BlockTxMaterialization }}
# weight: 10
log_query = {{ .RPCAdmission.ClassLimits.LogQuery }}
# weight: 10
evm_execution = {{ .RPCAdmission.ClassLimits.EVMExecution }}
# weight: 20
trace = {{ .RPCAdmission.ClassLimits.Trace }}
# weight: 2
broadcast = {{ .RPCAdmission.ClassLimits.Broadcast }}
# weight: 1
subscription = {{ .RPCAdmission.ClassLimits.Subscription }}

[rpc_admission.class_timeouts]
# Maximum time a request waits for admission. Zero performs a non-blocking attempt.
cheap_read = "{{ .RPCAdmission.ClassTimeouts.CheapRead }}"
normal_read = "{{ .RPCAdmission.ClassTimeouts.NormalRead }}"
search_index = "{{ .RPCAdmission.ClassTimeouts.SearchIndex }}"
block_tx_materialization = "{{ .RPCAdmission.ClassTimeouts.BlockTxMaterialization }}"
log_query = "{{ .RPCAdmission.ClassTimeouts.LogQuery }}"
evm_execution = "{{ .RPCAdmission.ClassTimeouts.EVMExecution }}"
trace = "{{ .RPCAdmission.ClassTimeouts.Trace }}"
broadcast = "{{ .RPCAdmission.ClassTimeouts.Broadcast }}"
subscription = "{{ .RPCAdmission.ClassTimeouts.Subscription }}"
`
