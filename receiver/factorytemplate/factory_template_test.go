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

package factorytemplate

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/spf13/viper"
	yaml "gopkg.in/yaml.v2"

	"github.com/census-instrumentation/opencensus-service/processor"
	"github.com/census-instrumentation/opencensus-service/receiver"
)

func TestNewTraceReceiverFactory(t *testing.T) {
	type args struct {
		receiverType  string
		newDefaultCfg func() interface{}
		newReceiver   func(interface{}) (receiver.TraceReceiver, error)
	}
	tests := []struct {
		name    string
		args    args
		want    interface{}
		wantErr error
	}{
		{
			name:    "empty receiverType",
			want:    nil,
			wantErr: ErrEmptyReciverType,
		},
		{
			name: "nil newDefaultCfg",
			args: args{
				receiverType: "friendlyReceiverTypeName",
			},
			want:    nil,
			wantErr: ErrNilNewDefaultCfg,
		},
		{
			name: "nil newReceiver",
			args: args{
				receiverType:  "friendlyReceiverTypeName",
				newDefaultCfg: newMockReceiverDefaultCfg,
			},
			want:    nil,
			wantErr: ErrNilNewReceiver,
		},
		{
			name: "happy path",
			args: args{
				receiverType:  "friendlyReceiverTypeName",
				newDefaultCfg: newMockReceiverDefaultCfg,
				newReceiver:   newMockReceiver,
			},
			want: &traceReceiverFactory{
				factory: factory{
					receiverType:  "friendlyReceiverTypeName",
					newDefaultCfg: newMockReceiverDefaultCfg,
				},
				newReceiver: newMockReceiver,
			},
			wantErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewTraceReceiverFactory(tt.args.receiverType, tt.args.newDefaultCfg, tt.args.newReceiver)
			if err != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got == nil && tt.want == nil {
				return
			}
			want := tt.want.(receiver.TraceReceiverFactory)
			if got.Type() != want.Type() {
				t.Errorf("New() = %v, want %v", got, want)
			}
		})
	}
}

func Test_traceReceiverFactory_NewFromViper(t *testing.T) {
	factories := []receiver.TraceReceiverFactory{
		checkedBuildReceiverFactory(t, "mockReceiver", newMockReceiverDefaultCfg, newMockReceiver),
		checkedBuildReceiverFactory(t, "altMockReceiver", altNewMockReceiverDefaultCfg, newMockReceiver),
	}

	v := viper.New()
	v.SetConfigFile("./testdata/test-config.yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("failed to read config file for test: %v", err)
	}

	var got []mockReceiverCfg
	for _, factory := range factories {
		r, _, err := factory.NewFromViper(v)
		if err != nil {
			t.Fatalf("failed to create receiver from factory: %v", err)
		}
		got = append(got, *r.(*mockReceiver).config)
	}

	want := []mockReceiverCfg{
		{
			Address: "0.0.0.0",
			Port:    616,
		},
		{
			Address: "altMockReceiverAddress",
			Port:    123,
		},
	}

	for i := range got {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("NewFromViper() at index %d got = %v, want = %v", i, got[i], want[i])
		}
	}
}

func Test_metricsReceiverFactory_NewFromViper(t *testing.T) {
	factory, err := NewMetricsReceiverFactory(
		"mockMetricsReceiver",
		newMockReceiverDefaultCfg,
		newMockMetricsReceiver,
	)
	if err != nil {
		t.Fatalf("failed to create factory: %v", err)
	}

	v := viper.New()
	r, _, err := factory.NewFromViper(v)
	if err != nil {
		t.Fatalf("failed to create receiver from factory: %v", err)
	}
	got := *r.(*mockReceiver).config

	want := mockReceiverCfg{
		Address: "mockReceiverAddress",
		Port:    616,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewFromViper() = %v, want = %v", got, want)
	}
}

func Test_factory_AddDefaultConfig(t *testing.T) {
	tests := []struct {
		factory receiver.TraceReceiverFactory
		want    mockReceiverCfg
	}{
		{
			factory: checkedBuildReceiverFactory(t, "mockReceiver", newMockReceiverDefaultCfg, newMockReceiver),
			want:    *newMockReceiverDefaultCfg().(*mockReceiverCfg),
		},
		{
			factory: checkedBuildReceiverFactory(t, "altMockReceiver", altNewMockReceiverDefaultCfg, newMockReceiver),
			want:    *altNewMockReceiverDefaultCfg().(*mockReceiverCfg),
		},
	}

	v := viper.New()
	for _, test := range tests {
		test.factory.AddDefaultConfig(v)
	}

	// Now check if defaults can be recreated from the added configs
	for i, test := range tests {
		var got mockReceiverCfg
		err := v.Sub("receivers").UnmarshalKey(test.factory.Type(), &got)
		if err != nil {
			t.Fatalf("failed to unmarshal default config for test[%d]: %v", i, err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("got = %v, want = %v for test[%d]", got, test.want, i)
		}
	}
}

func Examplefactory_AddDefaultConfig() {
	templateArgs := []struct {
		receiverType  string
		newDefaultCfg func() interface{}
		newReceiver   func(interface{}) (receiver.TraceReceiver, error)
	}{
		{
			receiverType:  "mockReceiver",
			newDefaultCfg: newMockReceiverDefaultCfg,
			newReceiver:   newMockReceiver,
		},
		{
			receiverType:  "altMockReceiver",
			newDefaultCfg: altNewMockReceiverDefaultCfg,
			newReceiver:   newMockReceiver,
		},
	}

	var factories []receiver.TraceReceiverFactory
	for _, args := range templateArgs {
		factory, _ := NewTraceReceiverFactory(args.receiverType, args.newDefaultCfg, args.newReceiver)
		factories = append(factories, factory)
	}

	v := viper.New()
	for _, factory := range factories {
		factory.AddDefaultConfig(v)
	}

	c := v.AllSettings()
	bs, _ := yaml.Marshal(c)
	fmt.Println(string(bs))
	// Output:
	// receivers:
	//   altmockreceiver:
	//     address: altMockReceiverAddress
	//     port: 1616
	//   mockreceiver:
	//     address: mockReceiverAddress
	//     port: 616
}

func checkedBuildReceiverFactory(
	t *testing.T,
	receiverType string,
	newDefaultCfg func() interface{},
	newReceiver func(interface{}) (receiver.TraceReceiver, error),
) receiver.TraceReceiverFactory {
	factory, err := NewTraceReceiverFactory(receiverType, newDefaultCfg, newReceiver)
	if err != nil {
		t.Fatalf("failed to build factory for %q: %v", receiverType, err)
	}
	return factory
}

type mockReceiverCfg struct {
	Address string `mapstructure:"address"`
	Port    uint16 `mapstructure:"Port"`
}

type mockReceiver struct {
	config *mockReceiverCfg
}

func newMockReceiverDefaultCfg() interface{} {
	return &mockReceiverCfg{
		Address: "mockReceiverAddress",
		Port:    616,
	}
}

func altNewMockReceiverDefaultCfg() interface{} {
	return &mockReceiverCfg{
		Address: "altMockReceiverAddress",
		Port:    1616,
	}
}

func newMockReceiver(cfg interface{}) (receiver.TraceReceiver, error) {
	return &mockReceiver{
		config: cfg.(*mockReceiverCfg),
	}, nil
}

func newMockMetricsReceiver(cfg interface{}) (receiver.MetricsReceiver, error) {
	return &mockReceiver{
		config: cfg.(*mockReceiverCfg),
	}, nil
}

var _ receiver.TraceReceiver = (*mockReceiver)(nil)
var _ receiver.MetricsReceiver = (*mockReceiver)(nil)

func (mr *mockReceiver) TraceSource() string {
	if mr == nil {
		panic("mockReceiver is nil")
	}
	return "mockReceiver"
}

func (mr *mockReceiver) StartTraceReception(ctx context.Context, nextProcessor processor.TraceDataProcessor) error {
	if mr == nil {
		panic("mockReceiver is nil")
	}
	return nil
}

func (mr *mockReceiver) StopTraceReception(ctx context.Context) error {
	if mr == nil {
		panic("mockReceiver is nil")
	}
	return nil
}

func (mr *mockReceiver) MetricsSource() string {
	if mr == nil {
		panic("mockReceiver is nil")
	}
	return "mockReceiver"
}

func (mr *mockReceiver) StartMetricsReception(ctx context.Context, nextProcessor processor.MetricsDataProcessor) error {
	if mr == nil {
		panic("mockReceiver is nil")
	}
	return nil
}

func (mr *mockReceiver) StopMetricsReception(ctx context.Context) error {
	if mr == nil {
		panic("mockReceiver is nil")
	}
	return nil
}
