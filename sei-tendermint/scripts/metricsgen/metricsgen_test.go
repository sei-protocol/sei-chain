package main_test

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"testing"

	metricsgen "github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen"
	"github.com/stretchr/testify/require"
)

const testDataDir = "./testdata"

func TestSimpleTemplate(t *testing.T) {
	m := metricsgen.ParsedMetricField{
		TypeName:           "HistogramVec",
		ConstructorPackage: "prometheus",
		ConstructorName:    "NewHistogramVec",
		OptsTypeName:       "HistogramOpts",
		FieldName:          "MyMetric",
		MetricName:         "request_count",
		Description:        "how many requests were made since the start of the process",
		Labels:             "\"first\",\"second\",\"third\"",
		LabelNames:         []string{"first", "second", "third"},

		MethodParams:     "first string, second string, third string",
		MethodArgs:       "first, second, third",
		MethodReturnType: "prometheus.Observer",
	}
	td := metricsgen.TemplateData{
		Package:         "mypack",
		StructName:      "Metrics",
		ConstructorName: "NewMetrics",
		ParsedMetrics:   []metricsgen.ParsedMetricField{m},
	}
	b := bytes.NewBuffer([]byte{})
	err := metricsgen.GenerateMetricsFile(b, td)
	if err != nil {
		t.Fatalf("unable to parse template %v", err)
	}
}

func TestFromData(t *testing.T) {
	infos, err := ioutil.ReadDir(testDataDir)
	if err != nil {
		t.Fatalf("unable to open file %v", err)
	}
	for _, dir := range infos {
		t.Run(dir.Name(), func(t *testing.T) {
			if !dir.IsDir() {
				t.Fatalf("expected file %s to be directory", dir.Name())
			}
			dirName := path.Join(testDataDir, dir.Name())
			pt, err := metricsgen.ParseMetricsDir(dirName, "Metrics")
			if err != nil {
				t.Fatalf("unable to parse from dir %q: %v", dir, err)
			}
			outFile := path.Join(dirName, "out.go")
			if err != nil {
				t.Fatalf("unable to open file %s: %v", outFile, err)
			}
			of, err := os.Create(outFile)
			if err != nil {
				t.Fatalf("unable to open file %s: %v", outFile, err)
			}
			defer os.Remove(outFile)
			if err := metricsgen.GenerateMetricsFile(of, pt); err != nil {
				t.Fatalf("unable to generate metrics file %s: %v", outFile, err)
			}
			if _, err := parser.ParseFile(token.NewFileSet(), outFile, nil, parser.AllErrors); err != nil {
				t.Fatalf("unable to parse generated file %s: %v", outFile, err)
			}
			bNew, err := ioutil.ReadFile(outFile)
			if err != nil {
				t.Fatalf("unable to read generated file %s: %v", outFile, err)
			}
			goldenFile := path.Join(dirName, "metrics.gen.go")
			bOld, err := ioutil.ReadFile(goldenFile)
			if err != nil {
				t.Fatalf("unable to read file %s: %v", goldenFile, err)
			}
			if !bytes.Equal(bNew, bOld) {
				t.Fatalf("newly generated code in file %s does not match golden file %s\n"+
					"if the output of the metricsgen tool is expected to change run the following make target: \n"+
					"\tmake metrics", outFile, goldenFile)
			}
		})
	}
}

func TestParseMetricsStruct(t *testing.T) {
	const pkgName = "mypkg"
	metricsTests := []struct {
		name          string
		shouldError   bool
		metricsStruct string
		expected      metricsgen.TemplateData
	}{
		{
			name: "basic",
			metricsStruct: `type Metrics struct {
				myGauge *prometheus.GaugeVec
			}`,
			expected: metricsgen.TemplateData{
				Package:         pkgName,
				StructName:      "Metrics",
				ConstructorName: "NewMetrics",
				ParsedMetrics: []metricsgen.ParsedMetricField{
					{
						TypeName:           "GaugeVec",
						ConstructorPackage: "prometheus",
						ConstructorName:    "NewGaugeVec",
						OptsTypeName:       "GaugeOpts",
						FieldName:          "myGauge",
						MetricName:         "my_gauge",
						MethodReturnType:   "prometheus.Gauge",
					},
				},
			},
		},
		{
			name: "histogram",
			metricsStruct: "type Metrics struct {\n" +
				"myHistogram *prometheus.HistogramVec `metrics_buckettype:\"exp\" metrics_bucketsizes:\"1, 100, .8\"`\n" +
				"}",
			expected: metricsgen.TemplateData{
				Package:         pkgName,
				StructName:      "Metrics",
				ConstructorName: "NewMetrics",
				ParsedMetrics: []metricsgen.ParsedMetricField{
					{
						TypeName:           "HistogramVec",
						ConstructorPackage: "prometheus",
						ConstructorName:    "NewHistogramVec",
						OptsTypeName:       "HistogramOpts",
						FieldName:          "myHistogram",
						MetricName:         "my_histogram",
						MethodReturnType:   "prometheus.Observer",

						HistogramOptions: metricsgen.HistogramOpts{
							BucketType:  "prometheus.ExponentialBuckets",
							BucketSizes: "1, 100, .8",
						},
					},
				},
			},
		},
		{
			name: "labeled name",
			metricsStruct: "type Metrics struct {\n" +
				"myCounter *prometheus.CounterVec `metrics_name:\"new_name\"`\n" +
				"}",
			expected: metricsgen.TemplateData{
				Package:         pkgName,
				StructName:      "Metrics",
				ConstructorName: "NewMetrics",
				ParsedMetrics: []metricsgen.ParsedMetricField{
					{
						TypeName:           "CounterVec",
						ConstructorPackage: "prometheus",
						ConstructorName:    "NewCounterVec",
						OptsTypeName:       "CounterOpts",
						FieldName:          "myCounter",
						MetricName:         "new_name",
						MethodReturnType:   "prometheus.Counter",
					},
				},
			},
		},
		{
			name: "metric labels",
			metricsStruct: "type Metrics struct {\n" +
				"myCounter *prometheus.CounterVec `metrics_labels:\"label1,label2\"`\n" +
				"}",
			expected: metricsgen.TemplateData{
				Package:         pkgName,
				StructName:      "Metrics",
				ConstructorName: "NewMetrics",
				ParsedMetrics: []metricsgen.ParsedMetricField{
					{
						TypeName:           "CounterVec",
						ConstructorPackage: "prometheus",
						ConstructorName:    "NewCounterVec",
						OptsTypeName:       "CounterOpts",
						FieldName:          "myCounter",
						MetricName:         "my_counter",
						Labels:             "\"label1\",\"label2\"",
						LabelNames:         []string{"label1", "label2"},
						MethodParams:       "label1 string, label2 string",
						MethodArgs:         "label1, label2",
						MethodReturnType:   "prometheus.Counter",
					},
				},
			},
		},
		{
			name: "ignore non-metric field",
			metricsStruct: `type Metrics struct {
				myCounter *prometheus.CounterVec
				nonMetric string
				}`,
			expected: metricsgen.TemplateData{
				Package:         pkgName,
				StructName:      "Metrics",
				ConstructorName: "NewMetrics",
				ParsedMetrics: []metricsgen.ParsedMetricField{
					{
						TypeName:           "CounterVec",
						ConstructorPackage: "prometheus",
						ConstructorName:    "NewCounterVec",
						OptsTypeName:       "CounterOpts",
						FieldName:          "myCounter",
						MetricName:         "my_counter",
						MethodReturnType:   "prometheus.Counter",
					},
				},
			},
		},
	}
	for _, testCase := range metricsTests {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			f, err := os.Create(filepath.Join(dir, "metrics.go"))
			if err != nil {
				t.Fatalf("unable to open file: %v", err)
			}
			pkgLine := fmt.Sprintf("package %s\n", pkgName)
			importClause := `
			import(
				"github.com/prometheus/client_golang/prometheus"
			)
			`

			_, err = io.WriteString(f, pkgLine)
			require.NoError(t, err)
			_, err = io.WriteString(f, importClause)
			require.NoError(t, err)
			_, err = io.WriteString(f, testCase.metricsStruct)
			require.NoError(t, err)

			td, err := metricsgen.ParseMetricsDir(dir, "Metrics")
			if testCase.shouldError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.expected, td)
			}
		})
	}
}

func TestParseAliasedMetric(t *testing.T) {
	aliasedData := `
			package mypkg

			import(
				mymetrics "github.com/prometheus/client_golang/prometheus"
			)
			type Metrics struct {
				m *mymetrics.GaugeVec
			}
			`
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "metrics.go"))
	if err != nil {
		t.Fatalf("unable to open file: %v", err)
	}
	_, err = io.WriteString(f, aliasedData)
	if err != nil {
		t.Fatalf("unable to write to file: %v", err)
	}
	td, err := metricsgen.ParseMetricsDir(dir, "Metrics")
	require.NoError(t, err)

	expected :=
		metricsgen.TemplateData{
			Package:         "mypkg",
			StructName:      "Metrics",
			ConstructorName: "NewMetrics",
			ParsedMetrics: []metricsgen.ParsedMetricField{
				{
					TypeName:           "GaugeVec",
					ConstructorPackage: "prometheus",
					ConstructorName:    "NewGaugeVec",
					OptsTypeName:       "GaugeOpts",
					FieldName:          "m",
					MetricName:         "m",
					MethodReturnType:   "prometheus.Gauge",
				},
			},
		}
	require.Equal(t, expected, td)
}

func TestParseLowercaseMetricsStruct(t *testing.T) {
	data := `
			package mypkg

			import(
				"github.com/prometheus/client_golang/prometheus"
			)
			type metrics struct {
				latency *prometheus.HistogramVec
			}
			`
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "metrics.go"))
	if err != nil {
		t.Fatalf("unable to open file: %v", err)
	}
	_, err = io.WriteString(f, data)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	td, err := metricsgen.ParseMetricsDir(dir, "metrics")
	require.NoError(t, err)
	require.Equal(t, "metrics", td.StructName)
	require.Equal(t, "newMetrics", td.ConstructorName)

	b := bytes.NewBuffer(nil)
	require.NoError(t, metricsgen.GenerateMetricsFile(b, td))
	require.Contains(t, b.String(), "var Global = newMetrics()")
	require.Contains(t, b.String(), "func newMetrics() *metrics")
	require.Contains(t, b.String(), "func (m *metrics) latencyAt() prometheus.Observer")
}
