package rpcadmission

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/stretchr/testify/require"
)

func TestReadConfigDefaults(t *testing.T) {
	got, err := ReadConfig(configtest.AppOpts{})
	require.NoError(t, err)
	require.Equal(t, DefaultConfig, got)
}

func TestReadConfigReadsEveryKey(t *testing.T) {
	opts := configtest.AppOpts{
		flagGlobalLimit:                                         "200",
		classLimitsPath + string(ClassCheapRead):                "21",
		classLimitsPath + string(ClassNormalRead):               "22",
		classLimitsPath + string(ClassSearchIndex):              "24",
		classLimitsPath + string(ClassBlockTxMaterialization):   "32",
		classLimitsPath + string(ClassLogQuery):                 "30",
		classLimitsPath + string(ClassEVMExecution):             "40",
		classLimitsPath + string(ClassTrace):                    "60",
		classLimitsPath + string(ClassBroadcast):                "42",
		classLimitsPath + string(ClassSubscription):             "25",
		classTimeoutsPath + string(ClassCheapRead):              "1ms",
		classTimeoutsPath + string(ClassNormalRead):             "2ms",
		classTimeoutsPath + string(ClassSearchIndex):            "3ms",
		classTimeoutsPath + string(ClassBlockTxMaterialization): "4ms",
		classTimeoutsPath + string(ClassLogQuery):               "5ms",
		classTimeoutsPath + string(ClassEVMExecution):           "6ms",
		classTimeoutsPath + string(ClassTrace):                  "7ms",
		classTimeoutsPath + string(ClassBroadcast):              "8ms",
		classTimeoutsPath + string(ClassSubscription):           "9ms",
	}

	got, err := ReadConfig(opts)
	require.NoError(t, err)
	require.Equal(t, Config{
		GlobalLimit: 200,
		ClassLimits: ClassLimits{
			CheapRead:              21,
			NormalRead:             22,
			SearchIndex:            24,
			BlockTxMaterialization: 32,
			LogQuery:               30,
			EVMExecution:           40,
			Trace:                  60,
			Broadcast:              42,
			Subscription:           25,
		},
		ClassTimeouts: ClassTimeouts{
			CheapRead:              time.Millisecond,
			NormalRead:             2 * time.Millisecond,
			SearchIndex:            3 * time.Millisecond,
			BlockTxMaterialization: 4 * time.Millisecond,
			LogQuery:               5 * time.Millisecond,
			EVMExecution:           6 * time.Millisecond,
			Trace:                  7 * time.Millisecond,
			Broadcast:              8 * time.Millisecond,
			Subscription:           9 * time.Millisecond,
		},
	}, got)
}

func TestReadConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		opts configtest.AppOpts
		want string
	}{
		{"bad cast", configtest.AppOpts{flagGlobalLimit: "many"}, flagGlobalLimit},
		{"negative global limit", configtest.AppOpts{flagGlobalLimit: -1}, flagGlobalLimit},
		{"global smaller than one method", configtest.AppOpts{flagGlobalLimit: 10}, "maximum single-method weight"},
		{"class below weight", configtest.AppOpts{classLimitsPath + string(ClassTrace): 10}, "class weight 20"},
		{"negative timeout", configtest.AppOpts{classTimeoutsPath + string(ClassNormalRead): "-1s"}, classTimeoutsPath + string(ClassNormalRead)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ReadConfig(test.opts)
			require.ErrorContains(t, err, test.want)
		})
	}
}
