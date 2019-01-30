package nodebatcher

import (
	"fmt"
	"testing"
	"time"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	resourcepb "github.com/census-instrumentation/opencensus-proto/gen-go/resource/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
)

type bucketIDTestInput struct {
	node     *commonpb.Node
	resource *resourcepb.Resource
	format   string
}

func TestGenBucketID(t *testing.T) {

	testCases := []struct {
		name   string
		match  bool
		input1 bucketIDTestInput
		input2 bucketIDTestInput
	}{
		{
			"different span formats",
			false,
			bucketIDTestInput{&commonpb.Node{ServiceInfo: &commonpb.ServiceInfo{Name: "svc"}}, nil, "oc"},
			bucketIDTestInput{&commonpb.Node{ServiceInfo: &commonpb.ServiceInfo{Name: "svc"}}, nil, "zipkin"},
		},
		{
			"identical but different node objects",
			true,
			bucketIDTestInput{&commonpb.Node{ServiceInfo: &commonpb.ServiceInfo{Name: "svc"}}, nil, "oc"},
			bucketIDTestInput{&commonpb.Node{ServiceInfo: &commonpb.ServiceInfo{Name: "svc"}}, nil, "oc"},
		},
		{
			"different nodes",
			false,
			bucketIDTestInput{&commonpb.Node{ServiceInfo: &commonpb.ServiceInfo{Name: "svc"}}, nil, "oc"},
			bucketIDTestInput{&commonpb.Node{ServiceInfo: &commonpb.ServiceInfo{Name: "svc2"}}, nil, "oc"},
		},
		{
			"different resources",
			false,
			bucketIDTestInput{
				&commonpb.Node{ServiceInfo: &commonpb.ServiceInfo{Name: "svc"}},
				&resourcepb.Resource{Labels: map[string]string{"a": "b"}},
				"oc",
			},
			bucketIDTestInput{
				&commonpb.Node{ServiceInfo: &commonpb.ServiceInfo{Name: "svc"}},
				&resourcepb.Resource{Labels: map[string]string{"a": "c"}},
				"oc",
			},
		},
		{
			"identical but different resources",
			true,
			bucketIDTestInput{
				&commonpb.Node{ServiceInfo: &commonpb.ServiceInfo{Name: "svc"}},
				&resourcepb.Resource{Labels: map[string]string{"a": "b"}},
				"oc",
			},
			bucketIDTestInput{
				&commonpb.Node{ServiceInfo: &commonpb.ServiceInfo{Name: "svc"}},
				&resourcepb.Resource{Labels: map[string]string{"a": "b"}},
				"oc",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sender := newTestSender()
			batcher := NewBatcher(sender)

			key1 := batcher.genBucketID(tc.input1.node, tc.input1.resource, tc.input1.format)
			key2 := batcher.genBucketID(tc.input2.node, tc.input2.resource, tc.input2.format)

			if tc.match != (key1 == key2) {
				t.Errorf("Keys should be matching=%v but were matching=%v", tc.match, key1 == key2)
			}
		})
	}
}

func TestConcurrentNodeAdds(t *testing.T) {
	sender := newTestSender()
	batcher := NewBatcher(sender, WithTimeout(250*time.Millisecond))
	requestCount := 2000
	spansPerRequest := 3
	for requestNum := 0; requestNum < requestCount; requestNum++ {
		spans := make([]*tracepb.Span, 0, spansPerRequest)
		for spanIndex := 0; spanIndex < spansPerRequest; spanIndex++ {
			spans = append(spans, &tracepb.Span{Name: getTestSpanName(requestNum, spanIndex)})
		}
		request := &agenttracepb.ExportTraceServiceRequest{
			Node: &commonpb.Node{
				ServiceInfo: &commonpb.ServiceInfo{Name: fmt.Sprintf("svc-%d", requestNum)},
			},
			Spans: spans,
		}
		go batcher.ProcessSpans(request, "oc")
	}

	err := sender.waitFor(requestCount*spansPerRequest, 3*time.Second)
	if err != nil {
		t.Errorf("failed to wait for sender %s", err)
	}
	if len(sender.spansReceivedByName) != requestCount*spansPerRequest {
		t.Errorf("Did not receive the correct number of spans. Got %d != expected %d.", len(sender.spansReceivedByName), requestCount*spansPerRequest)
		return
	}

	for requestNum := 0; requestNum < requestCount; requestNum++ {
		for spanIndex := 0; spanIndex < spansPerRequest; spanIndex++ {
			name := getTestSpanName(requestNum, spanIndex).Value
			if sender.spansReceivedByName[name] == nil {
				t.Errorf("Did not receive span %s.", name)
				return
			}
		}
	}
}

func TestBucketRemove(t *testing.T) {
	sender := newTestSender()
	tickTime := 50 * time.Millisecond
	removeAfterTicks := 2
	batcher := NewBatcher(
		sender,
		WithTimeout(50*time.Millisecond),
		WithTickTime(tickTime),
		WithRemoveAfterTicks(removeAfterTicks),
	)
	spansPerRequest := 3
	spans := make([]*tracepb.Span, 0, spansPerRequest)
	for spanIndex := 0; spanIndex < spansPerRequest; spanIndex++ {
		spans = append(spans, &tracepb.Span{Name: getTestSpanName(0, spanIndex)})
	}
	request := &agenttracepb.ExportTraceServiceRequest{
		Node: &commonpb.Node{
			ServiceInfo: &commonpb.ServiceInfo{Name: "svc"},
		},
		Spans: spans,
	}
	batcher.ProcessSpans(request, "oc")

	err := sender.waitFor(spansPerRequest, 1*time.Second)
	if err != nil {
		t.Errorf("failed to wait for sender %s", err)
	}

	if batcher.getBucket(batcher.genBucketID(request.Node, nil, "oc")) == nil {
		t.Errorf("Bucket should exist but does not.")
	}

	// Doesn't seem to be a great way to test this without waiting
	<-time.After(2 * time.Duration(removeAfterTicks) * tickTime)

	if batcher.getBucket(batcher.genBucketID(request.Node, nil, "oc")) != nil {
		t.Errorf("Bucket should be deleted but is not.")
	}
}

func TestConcurrentBatchAdds(t *testing.T) {
	sender := newTestSender()
	batcher := NewBatcher(sender, WithSendBatchSize(128))
	requestCount := 10000
	spansPerRequest := 3
	for requestNum := 0; requestNum < requestCount; requestNum++ {
		spans := make([]*tracepb.Span, 0, spansPerRequest)
		for spanIndex := 0; spanIndex < spansPerRequest; spanIndex++ {
			spans = append(spans, &tracepb.Span{Name: getTestSpanName(requestNum, spanIndex)})
		}
		request := &agenttracepb.ExportTraceServiceRequest{
			Node: &commonpb.Node{
				ServiceInfo: &commonpb.ServiceInfo{Name: "svc"},
			},
			Spans: spans,
		}
		go batcher.ProcessSpans(request, "oc")
	}

	err := sender.waitFor(requestCount*spansPerRequest, 2*time.Second)
	if err != nil {
		t.Errorf("failed to wait for sender %s", err)
	}
	if len(sender.spansReceivedByName) != requestCount*spansPerRequest {
		t.Errorf("Did not receive the correct number of spans. %d != %d", sender.spansReceivedByName, requestCount*spansPerRequest)
	}

	for requestNum := 0; requestNum < requestCount; requestNum++ {
		for spanIndex := 0; spanIndex < spansPerRequest; spanIndex++ {
			if name := sender.spansReceivedByName[getTestSpanName(requestNum, spanIndex).Value]; name == nil {
				t.Errorf("Did not receive span %s.", name)
			}
		}
	}
}

func BenchmarkConcurrentBatchAdds(b *testing.B) {
	sender := newTestSender()
	batcher := NewBatcher(sender)
	// We will send 1001 requests with 3 spans each, which should send
	// 250 batches of 12 spans, and 1 batch of 3 spans
	request := &agenttracepb.ExportTraceServiceRequest{
		Node: &commonpb.Node{
			ServiceInfo: &commonpb.ServiceInfo{Name: "svc"},
		},
		Spans: []*tracepb.Span{
			&tracepb.Span{Name: getTestSpanName(0, 1)},
			&tracepb.Span{Name: getTestSpanName(0, 2)},
			&tracepb.Span{Name: getTestSpanName(0, 3)},
		},
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			batcher.ProcessSpans(request, "oc")
		}
	})

	b.ReportAllocs()
}

func getTestSpanName(requestNum, index int) *tracepb.TruncatableString {
	return &tracepb.TruncatableString{
		Value: fmt.Sprintf("test-span-%d-%d", requestNum, index),
	}
}

type testSender struct {
	reqChan             chan *agenttracepb.ExportTraceServiceRequest
	batchesReceived     int
	spansReceived       int
	spansReceivedByName map[string]*tracepb.Span
}

func newTestSender() *testSender {
	return &testSender{
		reqChan:             make(chan *agenttracepb.ExportTraceServiceRequest, 100),
		spansReceivedByName: make(map[string]*tracepb.Span),
	}
}

func (ts *testSender) ProcessSpans(request *agenttracepb.ExportTraceServiceRequest, spanFormat string) (uint64, error) {
	ts.reqChan <- request
	return 0, nil
}

func (ts *testSender) waitFor(spans int, timeout time.Duration) error {
	for {
		select {
		case request := <-ts.reqChan:
			for _, span := range request.Spans {
				ts.spansReceivedByName[span.Name.Value] = span
			}
			ts.batchesReceived = ts.batchesReceived + 1
			ts.spansReceived = ts.spansReceived + len(request.Spans)
			if ts.spansReceived == spans {
				return nil
			}
		case <-time.After(timeout):
			return fmt.Errorf("Timed out waiting for spans.")
		}
	}
}
