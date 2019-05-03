// Copyright 2019, OpenCensus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configv2

import (
	"reflect"
	"testing"

	"github.com/census-instrumentation/opencensus-service/exporter"
	"github.com/census-instrumentation/opencensus-service/internal/models"
	"github.com/census-instrumentation/opencensus-service/processor"
	"github.com/census-instrumentation/opencensus-service/receiver"
)

func assertEqual(t *testing.T, actual, expected interface{}, errorMsg string) {
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s: expected %v, actual is %v", errorMsg, expected, actual)
	}
}

// ExampleReceiverCfg: for testing purposes we are defining an example config and factory
// for "examplereceiver" receiver type.
type ExampleReceiverCfg struct {
	models.ReceiverCommon `mapstructure:",squash"` // squash ensures fields are correctly decoded in embedded struct
	ExtraSetting          string                   `mapstructure:"extra"`
}

type ExampleReceiverFactory struct {
}

// Type gets the type of the Receiver config created by this factory.
func (f *ExampleReceiverFactory) Type() string {
	return "examplereceiver"
}

// CreateDefaultConfig creates the default configuration for the Receiver.
func (f *ExampleReceiverFactory) CreateDefaultConfig() models.ReceiverCfg {
	return &ExampleReceiverCfg{
		ReceiverCommon: models.ReceiverCommon{
			Type:    "examplereceiver",
			Name:    "examplereceiver",
			Address: "localhost",
			Port:    1000,
			Enabled: false,
		},
		ExtraSetting: "some string",
	}
}

func (f *ExampleReceiverFactory) CreateTraceReceiver(cfg models.ReceiverCfg) (*receiver.TraceReceiver, error) {
	// Not used for this test, just return nil
	return nil, nil
}

func (f *ExampleReceiverFactory) CreateMetricsReceiver(cfg models.ReceiverCfg) (*receiver.MetricsReceiver, error) {
	// Not used for this test, just return nil
	return nil, nil
}

// MultiProtoReceiverCfg: for testing purposes we are defining an example multi protocol
// config and factory for "multireceiver" receiver type.
type MultiProtoReceiverCfg struct {
	models.MultiReceiverCommon `mapstructure:",squash"`   // squash ensures fields are correctly decoded in embedded struct
	Protocols                  []MultiProtoReceiverOneCfg `mapstructure:"protocols"`
}

type MultiProtoReceiverOneCfg struct {
	models.ProtocolReceiver `mapstructure:",squash"` // squash ensures fields are correctly decoded in embedded struct
	ExtraSetting            string                   `mapstructure:"extra"`
}

type MultiProtoReceiverFactory struct {
}

// Type gets the type of the Receiver config created by this factory.
func (f *MultiProtoReceiverFactory) Type() string {
	return "multireceiver"
}

// CreateDefaultConfig creates the default configuration for the Receiver.
func (f *MultiProtoReceiverFactory) CreateDefaultConfig() models.ReceiverCfg {
	return &MultiProtoReceiverCfg{
		MultiReceiverCommon: models.MultiReceiverCommon{
			Type: "multireceiver",
			Name: "multireceiver",
		},
		Protocols: []MultiProtoReceiverOneCfg{
			{
				ProtocolReceiver: models.ProtocolReceiver{
					Protocol: "http",
					Enabled:  false,
					Address:  "example.com",
					Port:     8888,
				},
				ExtraSetting: "extra string 1",
			},
			{
				ProtocolReceiver: models.ProtocolReceiver{
					Protocol: "tcp",
					Enabled:  false,
					Address:  "omnition.com",
					Port:     9999,
				},
				ExtraSetting: "extra string 2",
			},
		},
	}
}

func (f *MultiProtoReceiverFactory) CreateTraceReceiver(cfg models.ReceiverCfg) (*receiver.TraceReceiver, error) {
	// Not used for this test, just return nil
	return nil, nil
}

func (f *MultiProtoReceiverFactory) CreateMetricsReceiver(cfg models.ReceiverCfg) (*receiver.MetricsReceiver, error) {
	// Not used for this test, just return nil
	return nil, nil
}

// ExampleExporterCfg: for testing purposes we are defining an example config and factory
// for "exampleexporter" exporter type.
type ExampleExporterCfg struct {
	models.ExporterCommon `mapstructure:",squash"` // squash ensures fields are correctly decoded in embedded struct
	ExtraSetting          string                   `mapstructure:"extra"`
}

func (cfg *ExampleExporterCfg) GetName() string {
	return cfg.Name
}

type ExampleExporterFactory struct {
}

// Type gets the type of the Exporter config created by this factory.
func (f *ExampleExporterFactory) Type() string {
	return "exampleexporter"
}

// CreateDefaultConfig creates the default configuration for the Exporter.
func (f *ExampleExporterFactory) CreateDefaultConfig() models.ExporterCfg {
	return &ExampleExporterCfg{
		ExporterCommon: models.ExporterCommon{
			Type:    "exampleexporter",
			Name:    "exampleexporter",
			Enabled: false,
		},
		ExtraSetting: "some export string",
	}
}

func (f *ExampleExporterFactory) CreateTraceExporter(cfg models.ExporterCfg) (*exporter.TraceExporter, error) {
	// Not used for this test, just return nil
	return nil, nil
}

func (f *ExampleExporterFactory) CreateMetricsExporter(cfg models.ExporterCfg) (*exporter.MetricsExporter, error) {
	// Not used for this test, just return nil
	return nil, nil
}

// ExampleOptionCfg: for testing purposes we are defining an example config and factory
// for "exampleoption" option type.
type ExampleOptionCfg struct {
	models.OptionCommon `mapstructure:",squash"` // squash ensures fields are correctly decoded in embedded struct
	ExtraSetting        string                   `mapstructure:"extra"`
}

type ExampleOptionFactory struct {
}

// Type gets the type of the Option config created by this factory.
func (f *ExampleOptionFactory) Type() string {
	return "exampleoption"
}

// CreateDefaultConfig creates the default configuration for the Option.
func (f *ExampleOptionFactory) CreateDefaultConfig() models.OptionCfg {
	return &ExampleOptionCfg{
		OptionCommon: models.OptionCommon{
			Type:    "exampleoption",
			Name:    "exampleoption",
			Enabled: false,
		},
		ExtraSetting: "some export string",
	}
}

func (f *ExampleOptionFactory) CreateTraceProcessor(cfg models.OptionCfg) (*processor.TraceProcessor, error) {
	// Not used for this test, just return nil
	return nil, nil
}

func (f *ExampleOptionFactory) CreateMetricsProcessor(cfg models.OptionCfg) (*processor.MetricsProcessor, error) {
	// Not used for this test, just return nil
	return nil, nil
}

// Register all factories
var _ = models.RegisterReceiverFactory(&ExampleReceiverFactory{})
var _ = models.RegisterReceiverFactory(&MultiProtoReceiverFactory{})
var _ = models.RegisterExporterFactory(&ExampleExporterFactory{})
var _ = models.RegisterOptionFactory(&ExampleOptionFactory{})

func TestDecodeConfig(t *testing.T) {

	var yaml = `
receivers:
  - type: examplereceiver
  - type: examplereceiver
    name: myreceiver
    address: "127.0.0.1"
    port: 12345
    enabled: true
    extra: "some string"

options:
  - type: exampleoption
    enabled: false

exporters:
  - type: exampleexporter
  - type: exampleexporter
    name: myexporter
    extra: "some export string 2"
    enabled: true

pipelines:
  traces:
    - name: default
      receivers:
        - name: examplereceiver
      ordered-operations:
        - name: exampleoption
      exporters:
        - name: exampleexporter
  metrics:
`
	v, err := readConfigFromYamlStr(yaml)
	if err != nil {
		t.Fatalf("unable to read yaml, %v", err)
	}

	config, err := Load(v)
	if err != nil {
		t.Fatalf("unable to load config, %v", err)
	}

	// Verify receivers
	assertEqual(t, len(config.Receivers), 2, "Incorrect receivers count")

	assertEqual(t, config.Receivers[0],
		&ExampleReceiverCfg{
			ReceiverCommon: models.ReceiverCommon{
				Type:    "examplereceiver",
				Name:    "examplereceiver",
				Address: "localhost",
				Port:    1000,
				Enabled: false,
			},
			ExtraSetting: "some string",
		}, "Did not load receiver 0 config correctly")

	assertEqual(t, config.Receivers[1],
		&ExampleReceiverCfg{
			ReceiverCommon: models.ReceiverCommon{
				Type:    "examplereceiver",
				Name:    "myreceiver",
				Address: "127.0.0.1",
				Port:    12345,
				Enabled: true,
			},
			ExtraSetting: "some string",
		}, "Did not load receiver 1 config correctly")

	// Verify exporters
	assertEqual(t, len(config.Exporters), 2, "Incorrect exporters count")

	assertEqual(t, config.Exporters[0],
		&ExampleExporterCfg{
			ExporterCommon: models.ExporterCommon{
				Type:    "exampleexporter",
				Name:    "exampleexporter",
				Enabled: false,
			},
			ExtraSetting: "some export string",
		}, "Did not load exporter 0 config correctly")

	assertEqual(t, config.Exporters[1],
		&ExampleExporterCfg{
			ExporterCommon: models.ExporterCommon{
				Type:    "exampleexporter",
				Name:    "myexporter",
				Enabled: true,
			},
			ExtraSetting: "some export string 2",
		}, "Did not load exporter 1 config correctly")

	// Verify Options
	assertEqual(t, len(config.Options), 1, "Incorrect options count")

	assertEqual(t, config.Options[0],
		&ExampleOptionCfg{
			OptionCommon: models.OptionCommon{
				Type:    "exampleoption",
				Name:    "exampleoption",
				Enabled: false,
			},
			ExtraSetting: "some export string",
		}, "Did not load option 0 config correctly")

	// Verify Pipelines
	assertEqual(t, len(config.Pipelines.Traces), 1, "Incorrect traces pipelines count")
	assertEqual(t, len(config.Pipelines.Metrics), 0, "Incorrect metrics pipelines count")

	assertEqual(t, config.Pipelines.Traces[0],
		&models.Pipeline{
			Name: "default",
			Receivers: []models.PipelineReceiver{
				{Name: "examplereceiver"},
			},
			Operations: []models.PipelineOperation{
				{Name: "exampleoption"},
			},
			Exporters: []models.PipelineExporter{
				{Name: "exampleexporter"},
			},
		}, "Did not load pipeline 0 config correctly")
}

func TestDecodeConfig_MultiProto(t *testing.T) {

	var yaml = `
receivers:
  - type: multireceiver
  - type: multireceiver
    name: myreceiver
    protocols:
      - protocol: http
        address: "127.0.0.1"
        port: 12345
        enabled: true
        extra: "some string 1"
      - protocol: tcp
        address: "0.0.0.0"
        port: 4567
        enabled: true
        extra: "some string 2"

options:
  - type: exampleoption
    enabled: false

exporters:
  - type: exampleexporter
    extra: "locahost:1010"

pipelines:
  traces:
    - name: default
      receivers:
        - name: myreceiver
      ordered-operations:
        - name: exampleoption
      exporters:
        - name: exampleexporter
  metrics:
    - name: default
      receivers:
        - name: myreceiver
      exporters:
        - name: exampleexporter
`
	v, err := readConfigFromYamlStr(yaml)
	if err != nil {
		t.Fatalf("unable to read yaml, %v", err)
	}

	config, err := Load(v)
	if err != nil {
		t.Fatalf("unable to load config, %v", err)
	}

	assertEqual(t, len(config.Receivers), 2, "Incorrect receivers count")

	assertEqual(t, config.Receivers[0],
		&MultiProtoReceiverCfg{
			MultiReceiverCommon: models.MultiReceiverCommon{
				Type: "multireceiver",
				Name: "multireceiver",
			},
			Protocols: []MultiProtoReceiverOneCfg{
				{
					ProtocolReceiver: models.ProtocolReceiver{
						Protocol: "http",
						Enabled:  false,
						Address:  "example.com",
						Port:     8888,
					},
					ExtraSetting: "extra string 1",
				},
				{
					ProtocolReceiver: models.ProtocolReceiver{
						Protocol: "tcp",
						Enabled:  false,
						Address:  "omnition.com",
						Port:     9999,
					},
					ExtraSetting: "extra string 2",
				},
			},
		}, "Did not load receiver 0 config correctly")

	assertEqual(t, config.Receivers[1],
		&MultiProtoReceiverCfg{
			MultiReceiverCommon: models.MultiReceiverCommon{
				Type: "multireceiver",
				Name: "myreceiver",
			},
			Protocols: []MultiProtoReceiverOneCfg{
				{
					ProtocolReceiver: models.ProtocolReceiver{
						Protocol: "http",
						Enabled:  true,
						Address:  "127.0.0.1",
						Port:     12345,
					},
					ExtraSetting: "some string 1",
				},
				{
					ProtocolReceiver: models.ProtocolReceiver{
						Protocol: "tcp",
						Enabled:  true,
						Address:  "0.0.0.0",
						Port:     4567,
					},
					ExtraSetting: "some string 2",
				},
			},
		}, "Did not load receiver 1 config correctly")
}

func TestDecodeConfig_Invalid(t *testing.T) {

	var testCases = []struct {
		failMsg  string          // message to print if test case fails
		expected configErrorCode // expected error (if nil any error is acceptable)
		yaml     string          // input config
	}{
		{
			failMsg: "empty config",
			yaml:    ``,
		},

		{
			failMsg: "missing all sections",
			yaml: `
receivers:
exporters:
options:
pipeline:
`,
		},

		{
			failMsg: "missing receivers",
			yaml: `
receivers:
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  traces:
    - name: default
`,
		},

		{
			failMsg: "missing exporters",
			yaml: `
receivers:
  - type: multireceiver
exporters:
options:
  - type: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: invalidreceivername
`,
		},

		{
			failMsg: "missing options",
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - type: exampleexporter
options:
pipelines:
  traces:
    - name: default
      receivers:
        - name: invalidreceivername
`,
		},

		{
			failMsg:  "invalid receiver reference",
			expected: errPipelineReceiverNotExists,
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: invalidreceivername
`,
		},

		{
			failMsg:  "missing receiver type",
			expected: errMissingReceiverType,
			yaml: `
receivers:
  - name: multireceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: somereceiver
`,
		},

		{
			failMsg:  "missing exporter type",
			expected: errMissingExporterType,
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - name: exampleexporter
options:
  - type: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: somereceiver
`,
		},

		{
			failMsg:  "missing option type",
			expected: errMissingOptionType,
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - type: exampleexporter
options:
  - name: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: somereceiver
`,
		},

		{
			expected: errMissingPipelineName,
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  traces:
    - receivers:
        - name: multireceiver
`,
		},

		{
			expected: errMissingPipelines,
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
`,
		},

		{
			expected: errPipelineMustHaveExporter,
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: multireceiver
`,
		},

		{
			expected: errPipelineMustHaveExporter,
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  metrics:
    - name: default
      receivers:
        - name: multireceiver
`,
		},

		{
			expected: errPipelineMustHaveReceiver,
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  metrics:
    - name: default
      exporters:
        - name: exampleexporter
`,
		},

		{
			expected: errPipelineExporterNotExists,
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  metrics:
    - name: default
      receivers:
        - name: multireceiver
      exporters:
        - name: nosuchexporter
`,
		},

		{
			expected: errPipelineOptionNotExists,
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: multireceiver
      ordered-operations:
        - name: nosuchoption
      exporters:
        - name: exampleexporter
`,
		},

		{
			expected: errPipelineMustHaveOperations,
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: multireceiver
      exporters:
        - name: exampleexporter
`,
		},

		{
			expected: errMetricPipelineCannotHaveOperations,
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  metrics:
    - name: default
      receivers:
        - name: multireceiver
      exporters:
        - name: exampleexporter
      ordered-operations:
        - name: exampleoption
`,
		},

		{
			failMsg: "invalid receiver name",
			yaml: `
receivers:
  - type: multireceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: [1,2,3]
      exporters:
        - name: exampleexporter
      ordered-operations:
        - name: exampleoption
`,
		},

		{
			expected: errUnknownReceiverType,
			failMsg:  "unknown receiver type",
			yaml: `
receivers:
  - type: nosuchreceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: multireceiver
      exporters:
        - name: exampleexporter
      ordered-operations:
        - name: exampleoption
`,
		},

		{
			expected: errUnknownExporterType,
			failMsg:  "unknown exporter type",
			yaml: `
receivers:
  - type: examplereceiver
exporters:
  - type: nosuchexporter
options:
  - type: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: multireceiver
      exporters:
        - name: exampleexporter
      ordered-operations:
        - name: exampleoption
`,
		},

		{
			expected: errUnmarshalError,
			failMsg:  "invalid receiver port",
			yaml: `
receivers:
  - type: examplereceiver
    port: "string for port"
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: examplereceiver
      exporters:
        - name: exampleexporter
      ordered-operations:
        - name: exampleoption
`,
		},

		{
			expected: errUnmarshalError,
			failMsg:  "invalid enabled bool value",
			yaml: `
receivers:
  - type: examplereceiver
exporters:
  - type: exampleexporter
    enabled: "string for bool"
options:
  - type: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: examplereceiver
      exporters:
        - name: exampleexporter
      ordered-operations:
        - name: exampleoption
`,
		},

		{
			expected: errUnknownOptionType,
			failMsg:  "unknown option type",
			yaml: `
receivers:
  - type: examplereceiver
exporters:
  - type: exampleexporter
options:
  - type: nosuchoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: examplereceiver
      exporters:
        - name: exampleexporter
      ordered-operations:
        - name: exampleoption
`,
		},

		{
			expected: errUnmarshalError,
			failMsg:  "invalid bool value",
			yaml: `
receivers:
  - type: examplereceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
    enabled: [2,3]
pipelines:
  traces:
    - name: default
      receivers:
        - name: examplereceiver
      exporters:
        - name: exampleexporter
      ordered-operations:
        - name: exampleoption
`,
		},

		{
			expected: errDuplicateReceiverName,
			failMsg:  "duplicate receiver",
			yaml: `
receivers:
  - type: examplereceiver
  - type: examplereceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
    enabled: true
pipelines:
  traces:
    - name: default
      receivers:
        - name: examplereceiver
      exporters:
        - name: exampleexporter
      ordered-operations:
        - name: exampleoption
`,
		},

		{
			expected: errDuplicateExporterName,
			failMsg:  "duplicate exporter",
			yaml: `
receivers:
  - type: examplereceiver
exporters:
  - type: exampleexporter
  - type: exampleexporter
options:
  - type: exampleoption
    enabled: true
pipelines:
  traces:
    - name: default
      receivers:
        - name: examplereceiver
      exporters:
        - name: exampleexporter
      ordered-operations:
        - name: exampleoption
`,
		},

		{
			expected: errDuplicateOptionName,
			failMsg:  "duplicate option",
			yaml: `
receivers:
  - type: examplereceiver
exporters:
  - type: exampleexporter
options:
  - type: exampleoption
    enabled: true
  - type: exampleoption
pipelines:
  traces:
    - name: default
      receivers:
        - name: examplereceiver
      exporters:
        - name: exampleexporter
      ordered-operations:
        - name: exampleoption
`,
		},
	}

	for _, test := range testCases {
		v, err := readConfigFromYamlStr(test.yaml)
		if err != nil {
			t.Fatalf("unable to read yaml, %v", err)
		}

		_, err = Load(v)
		if err == nil {
			t.Errorf("expected error but succedded on invalid config case: %s", test.failMsg)
		} else if test.expected != 0 {
			cfgErr, ok := err.(*configError)
			if !ok {
				t.Errorf("expected config error code %v but got a different error '%v' on invalid config case: %s", test.expected, err, test.failMsg)
			} else {
				if cfgErr.code != test.expected {
					t.Errorf("expected config error code %v but got error code %v on invalid config case: %s", test.expected, cfgErr.code, test.failMsg)
				}

				if cfgErr.msg == "" {
					t.Errorf("returned config error %v with empty msg field", cfgErr.code)
				}
			}
		}
	}
}
