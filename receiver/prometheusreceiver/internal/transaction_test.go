package internal

import (
	"context"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/scrape"
	"reflect"
	"testing"
	"time"
)



func Test_transaction(t *testing.T) {
	ms := &mockMetadataSvc{
		caches:map[string]*mockMetadataCache{
			"test_localhost:8080":{data: map[string]scrape.MetricMetadata{}},
		},
	}
	
	t.Run("Commit Without Adding", func(t *testing.T){
		mcon := NewMockConsumer()
		tr := newTransaction(context.Background(), ms, mcon, testLogger)
		if got := tr.Commit(); got != nil {
			t.Errorf("expecting nil from Commit() but got err %v", got)
		}
	})

	t.Run("Rollback dose nothing", func(t *testing.T){
		mcon := NewMockConsumer()
		tr := newTransaction(context.Background(), ms, mcon, testLogger)
		if got := tr.Rollback(); got != nil {
			t.Errorf("expecting nil from Rollback() but got err %v", got)
		}
	})

	badLabels := labels.Labels([]labels.Label{{"foo", "bar"}})
	t.Run("Add One No Target", func(t *testing.T){
		mcon := NewMockConsumer()
		tr := newTransaction(context.Background(), ms, mcon, testLogger)
		if _, got := tr.Add(badLabels, time.Now().Unix()*1000, 1.0); got == nil {
			t.Errorf("expecting error from Add() but got nil")
		}
	})

	jobNotFoundLb := labels.Labels([]labels.Label{{"instance", "localhost:8080"}, {"job", "test2"}, {"foo", "bar"}})
	t.Run("Add One Job not found", func(t *testing.T){
		mcon := NewMockConsumer()
		tr := newTransaction(context.Background(), ms, mcon, testLogger)
		if _, got := tr.Add(jobNotFoundLb, time.Now().Unix()*1000, 1.0); got == nil {
			t.Errorf("expecting error from Add() but got nil")
		}
	})

	goodLabels := labels.Labels([]labels.Label{{"instance", "localhost:8080"}, {"job", "test"}, {"__name__", "foo"}})
	t.Run("Add One Good", func(t *testing.T){
		mcon := NewMockConsumer()
		tr := newTransaction(context.Background(), ms, mcon, testLogger)
		if _, got := tr.Add(goodLabels, time.Now().Unix()*1000, 1.0); got != nil {
			t.Errorf("expecting error == nil from Add() but got: %v\n", got)
		}
		if got := tr.Commit(); got != nil {
			t.Errorf("expecting nil from Commit() but got err %v", got)
		}
		
		expected := createNode("test", "localhost:8080", "http")
		md := <- mcon.Metrics
		if !reflect.DeepEqual(md.Node, expected) {
			t.Errorf("generated node %v and expected node %v is different\n", md.Node, expected)
		}
		
		if len(md.Metrics) != 1 {
			t.Errorf("expecting one metrics, but got %v\n", len(md.Metrics))
		}
		
	})
	
	
}