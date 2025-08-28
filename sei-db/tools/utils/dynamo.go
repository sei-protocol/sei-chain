package utils

import (
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
)

// DynamoDBClient wraps the DynamoDB service with common operations
type DynamoDBClient struct {
	client *dynamodb.DynamoDB
	table  string
}

// NewDynamoDBClient creates a new DynamoDB client
func NewDynamoDBClient(tableName, awsRegion string) (*DynamoDBClient, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(awsRegion),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	return &DynamoDBClient{
		client: dynamodb.New(sess),
		table:  tableName,
	}, nil
}

// StateSizeAnalysis represents the analysis data to be stored in DynamoDB
type StateSizeAnalysis struct {
	BlockHeight       int64  `json:"block_height" dynamodbav:"block_height"`
	ModuleName        string `json:"module_name" dynamodbav:"module_name"`
	TotalNumKeys      int    `json:"total_num_keys" dynamodbav:"total_num_keys"`
	TotalKeySize      int64  `json:"total_key_size" dynamodbav:"total_key_size"`
	TotalValueSize    int64  `json:"total_value_size" dynamodbav:"total_value_size"`
	TotalSize         int64  `json:"total_size" dynamodbav:"total_size"`
	PrefixBreakdown   string `json:"prefix_breakdown" dynamodbav:"prefix_breakdown"`
	ContractBreakdown string `json:"contract_breakdown" dynamodbav:"contract_breakdown"`
}

// ContractSizeEntry represents individual contract size data
type ContractSizeEntry struct {
	Address   string `json:"address"`
	TotalSize int64  `json:"total_size"`
	KeyCount  int    `json:"key_count"`
}

// ExportStateSizeAnalysis exports a single module analysis to DynamoDB
func (d *DynamoDBClient) ExportStateSizeAnalysis(analysis *StateSizeAnalysis) error {
	// Convert to DynamoDB attribute values
	item, err := dynamodbattribute.MarshalMap(analysis)
	if err != nil {
		return fmt.Errorf("failed to marshal analysis for module %s: %w", analysis.ModuleName, err)
	}

	// Write to DynamoDB
	input := &dynamodb.PutItemInput{
		Item:      item,
		TableName: aws.String(d.table),
	}

	_, err = d.client.PutItem(input)
	if err != nil {
		return fmt.Errorf("failed to write to DynamoDB for module %s: %w", analysis.ModuleName, err)
	}

	return nil
}

// ExportMultipleAnalyses exports multiple analyses sequentially
func (d *DynamoDBClient) ExportMultipleAnalyses(analyses []*StateSizeAnalysis) error {
	for i, analysis := range analyses {
		if err := d.ExportStateSizeAnalysis(analysis); err != nil {
			return fmt.Errorf("failed to export analysis %d (module: %s): %w", i, analysis.ModuleName, err)
		}
	}
	return nil
}

// CreateStateSizeAnalysis creates a new StateSizeAnalysis
func CreateStateSizeAnalysis(blockHeight int64, moduleName string, result interface{}) *StateSizeAnalysis {

	// Type assertion to access the fields directly
	moduleResult := result.(struct {
		ModuleName        string
		TotalNumKeys      int
		TotalKeySize      int64
		TotalValueSize    int64
		TotalSize         int64
		KeySizeByPrefix   map[string]int64
		ValueSizeByPrefix map[string]int64
		TotalSizeByPrefix map[string]int64
		NumKeysByPrefix   map[string]int64
		ContractSizes     map[string]*ContractSizeEntry
	})

	// Convert raw data to JSON strings for DynamoDB storage
	prefixBreakdown := map[string]interface{}{
		"key_sizes":   moduleResult.KeySizeByPrefix,
		"value_sizes": moduleResult.ValueSizeByPrefix,
		"total_sizes": moduleResult.TotalSizeByPrefix,
		"key_counts":  moduleResult.NumKeysByPrefix,
	}
	prefixJSON, _ := json.Marshal(prefixBreakdown)

	var contractSlice []ContractSizeEntry
	for _, contract := range moduleResult.ContractSizes {
		contractSlice = append(contractSlice, *contract)
	}
	contractJSON, _ := json.Marshal(contractSlice)

	return &StateSizeAnalysis{
		BlockHeight:       blockHeight,
		ModuleName:        moduleName,
		TotalNumKeys:      moduleResult.TotalNumKeys,
		TotalKeySize:      moduleResult.TotalKeySize,
		TotalValueSize:    moduleResult.TotalValueSize,
		TotalSize:         moduleResult.TotalSize,
		PrefixBreakdown:   string(prefixJSON),
		ContractBreakdown: string(contractJSON),
	}
}
