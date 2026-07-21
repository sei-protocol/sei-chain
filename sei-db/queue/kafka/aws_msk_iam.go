package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	v4 "github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/segmentio/kafka-go/sasl"
)

const (
	mskIAMVersion      = "2020_10_22"
	mskIAMService      = "kafka-cluster"
	mskIAMAction       = "kafka-cluster:Connect"
	mskIAMVersionKey   = "version"
	mskIAMHostKey      = "host"
	mskIAMUserAgentKey = "user-agent"
	mskIAMActionKey    = "action"
	mskIAMQueryAction  = "Action"
)

var mskIAMUserAgent = fmt.Sprintf("sei-chain/cryptosim/aws_msk_iam/%s", runtime.Version())

type awsMSKIAMMechanism struct {
	signer   *v4.Signer
	region   string
	signTime time.Time
	expiry   time.Duration
}

var _ sasl.Mechanism = (*awsMSKIAMMechanism)(nil)
var _ sasl.StateMachine = (*awsMSKIAMMechanism)(nil)

func newAWSMSKIAMMechanism(cfg WriterConfig) (sasl.Mechanism, error) {
	sess, err := session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region: aws.String(cfg.Region),
		},
		SharedConfigState: session.SharedConfigEnable,
	})
	if err != nil {
		return nil, fmt.Errorf("create aws session for msk iam: %w", err)
	}

	region := cfg.Region
	if region == "" && sess.Config.Region != nil {
		region = aws.StringValue(sess.Config.Region)
	}
	if region == "" {
		return nil, fmt.Errorf("kafka region is required for aws-msk-iam")
	}

	return &awsMSKIAMMechanism{
		signer: v4.NewSigner(sess.Config.Credentials),
		region: region,
		expiry: 5 * time.Minute,
	}, nil
}

func (m *awsMSKIAMMechanism) Name() string {
	return "AWS_MSK_IAM"
}

func (m *awsMSKIAMMechanism) Start(ctx context.Context) (sasl.StateMachine, []byte, error) {
	meta := sasl.MetadataFromContext(ctx)
	if meta == nil {
		return nil, nil, errors.New("missing sasl metadata")
	}

	query := url.Values{
		mskIAMQueryAction: {mskIAMAction},
	}
	signURL := url.URL{
		Scheme:   "kafka",
		Host:     meta.Host,
		Path:     "/",
		RawQuery: query.Encode(),
	}

	req, err := http.NewRequest(http.MethodGet, signURL.String(), nil)
	if err != nil {
		return nil, nil, err
	}

	signTime := m.signTime
	if signTime.IsZero() {
		signTime = time.Now()
	}

	expiry := m.expiry
	if expiry == 0 {
		expiry = 5 * time.Minute
	}

	header, err := m.signer.Presign(req, nil, mskIAMService, m.region, expiry, signTime)
	if err != nil {
		return nil, nil, err
	}

	signed := map[string]string{
		mskIAMVersionKey:   mskIAMVersion,
		mskIAMHostKey:      signURL.Host,
		mskIAMUserAgentKey: mskIAMUserAgent,
		mskIAMActionKey:    mskIAMAction,
	}
	for key, vals := range header {
		signed[strings.ToLower(key)] = vals[0]
	}
	for key, vals := range req.URL.Query() {
		signed[strings.ToLower(key)] = vals[0]
	}

	payload, err := json.Marshal(signed)
	if err != nil {
		return nil, nil, err
	}
	return m, payload, nil
}

func (m *awsMSKIAMMechanism) Next(context.Context, []byte) (bool, []byte, error) {
	return true, nil, nil
}
