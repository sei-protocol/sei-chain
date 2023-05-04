package v1_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/suite"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	v1 "github.com/sei-protocol/sei-chain/oracle/price-feeder/router/v1"

	"github.com/cosmos/cosmos-sdk/telemetry"
)

var (
	_ v1.Oracle = (*mockOracle)(nil)

	mockPrices = sdk.DecCoins{
		sdk.NewDecCoinFromDec("ATOM", sdk.MustNewDecFromStr("34.84")),
		sdk.NewDecCoinFromDec("UMEE", sdk.MustNewDecFromStr("4.21")),
	}
)

type mockOracle struct{}

func (m mockOracle) GetLastPriceSyncTimestamp() time.Time {
	return time.Now()
}

func (m mockOracle) GetPrices() sdk.DecCoins {
	return mockPrices
}

type mockMetrics struct{}

func (mockMetrics) Gather(format string) (telemetry.GatherResponse, error) {
	return telemetry.GatherResponse{}, nil
}

type RouterTestSuite struct {
	suite.Suite

	mux    *mux.Router
	router *v1.Router
}

// SetupSuite executes once before the suite's tests are executed.
func (rts *RouterTestSuite) SetupSuite() {
	mux := mux.NewRouter()
	cfg := config.Config{
		Server: config.Server{
			AllowedOrigins: []string{},
			VerboseCORS:    false,
		},
	}

	r := v1.New(zerolog.Nop(), cfg, mockOracle{}, mockMetrics{})
	r.RegisterRoutes(mux, v1.APIPathPrefix)

	rts.mux = mux
	rts.router = r
}

func TestServiceTestSuite(t *testing.T) {
	suite.Run(t, new(RouterTestSuite))
}

func (rts *RouterTestSuite) executeRequest(req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	rts.mux.ServeHTTP(rr, req)

	return rr
}

func (rts *RouterTestSuite) TestHealthz() {
	req, err := http.NewRequest("GET", "/api/v1/healthz", nil)
	rts.Require().NoError(err)

	response := rts.executeRequest(req)
	rts.Require().Equal(http.StatusOK, response.Code)

	var respBody map[string]interface{}
	rts.Require().NoError(json.Unmarshal(response.Body.Bytes(), &respBody))
	rts.Require().Equal(respBody["status"], v1.StatusAvailable)
}

func (rts *RouterTestSuite) TestPrices() {
	req, err := http.NewRequest("GET", "/api/v1/prices", nil)
	rts.Require().NoError(err)

	response := rts.executeRequest(req)
	rts.Require().Equal(http.StatusOK, response.Code)

	var respBody v1.PricesResponse
	rts.Require().NoError(json.Unmarshal(response.Body.Bytes(), &respBody))
	rts.Require().Equal(respBody.Prices["ATOM"], mockPrices.AmountOf("ATOM"))
	rts.Require().Equal(respBody.Prices["UMEE"], mockPrices.AmountOf("UMEE"))
	rts.Require().Equal(respBody.Prices["FOO"], sdk.Dec{})
}
