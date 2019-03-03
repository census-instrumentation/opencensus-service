package postgresreceiver

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"log"
	"math/rand"
	"time"

	commonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/census-instrumentation/opencensus-service/data"
	"github.com/census-instrumentation/opencensus-service/internal"
	"github.com/census-instrumentation/opencensus-service/processor"
	_ "github.com/lib/pq"
)

type PostgresReceiver struct {
	db *sql.DB
}

func New() *PostgresReceiver {
	connStr := "user=postgres dbname=postgres sslmode=disable"
	db, err := sql.Open( /* driver = */ "postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec("create extension if not exists google_insights")
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Connected to postgres. Extension created.")
	return &PostgresReceiver{db: db}
}

func (pgr *PostgresReceiver) StartTraceReception(ctx context.Context, nextProcessor processor.TraceDataProcessor) error {
	go func() {
		for range time.Tick(10 * time.Second) {
			pgr.ProcessExecutionPlan(nextProcessor)
		}

	}()
	return nil
}

func (pgr *PostgresReceiver) StopTraceReception(ctx context.Context) error {
	return pgr.db.Close()
}

func (pgr *PostgresReceiver) ProcessExecutionPlan(nextProcessor processor.TraceDataProcessor) {
	rows, err := pgr.db.Query("select * from google_trace()/* DO NOT TRACE */")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var counter int
		var plan_str string
		if err := rows.Scan(&counter, &plan_str); err != nil {
			log.Fatal(err)
		}
		log.Println(counter)
		log.Println(plan_str)

		var message interface{}
		err := json.Unmarshal([]byte(plan_str), &message)
		if err != nil {
			log.Println("Unmarshal execution plan failed: ", err)
			continue
		}
		connection_id, spans := parseExecutionPlan(message)
		td := data.TraceData{
			Node: &commonpb.Node{
				Identifier: &commonpb.ProcessIdentifier{
					HostName: "PostgreSQL",
					Pid:      uint32(connection_id),
				},
			},
			Spans: spans,
		}
		nextProcessor.ProcessTraceData(context.Background(), td)
	}
}

func parseExecutionPlan(message interface{}) (int64, []*tracepb.Span) {
	plan := message.(map[string]interface{})

	trace_id := generateTraceId()
	span_id := generateSpanId()

	start_timestamp := plan["start timestamp"].(float64)
	duration := plan["duration"].(float64)
	start_time := timestampToTime(start_timestamp)
	end_time := timestampToTime(start_timestamp + duration)

	attributes := make(map[string]*tracepb.AttributeValue)
	attributes["query"] = stringToAttributeValue(plan["Query Text"].(string))
	attributes["username"] = stringToAttributeValue(plan["username"].(string))
	attributes["session_username"] = stringToAttributeValue(plan["session_username"].(string))

	backend_pid := int64(plan["connection_id"].(float64))
	attributes["connection_id"] = int64ToAttributeValue(backend_pid)
	attributes["database_name"] = stringToAttributeValue(plan["database_name"].(string))

	root_span := &tracepb.Span{
		TraceId:    trace_id,
		SpanId:     span_id,
		Name:       &tracepb.TruncatableString{Value: "CloudSQLQuery"},
		StartTime:  internal.TimeToTimestamp(start_time),
		EndTime:    internal.TimeToTimestamp(end_time),
		Attributes: &tracepb.Span_Attributes{AttributeMap: attributes},
	}

	_, spans := parseChildPlan(plan["Plan"], start_time, trace_id, span_id)
	spans = append(spans, root_span)
	return backend_pid, spans
}

func generateTraceId() []byte {
	trace_id := make([]byte, 16)
	binary.LittleEndian.PutUint64(trace_id[0:8], rand.Uint64())
	binary.LittleEndian.PutUint64(trace_id[8:16], rand.Uint64())
	return trace_id
}

func generateSpanId() []byte {
	span_id := make([]byte, 8)
	binary.LittleEndian.PutUint64(span_id[:], rand.Uint64())
	return span_id
}

func timestampToTime(timestamp float64) time.Time {
	sec := int64(timestamp)
	nsec := int64((timestamp - float64(sec)) * 1e9)
	return time.Unix(sec, nsec)
}

func stringToAttributeValue(val string) *tracepb.AttributeValue {
	return &tracepb.AttributeValue{
		Value: &tracepb.AttributeValue_StringValue{
			StringValue: &tracepb.TruncatableString{
				Value: val,
			},
		},
	}
}

func int64ToAttributeValue(val int64) *tracepb.AttributeValue {
	return &tracepb.AttributeValue{
		Value: &tracepb.AttributeValue_IntValue{
			IntValue: val,
		},
	}
}

func parseChildPlan(plan interface{}, trace_start_time time.Time, trace_id []byte, parent_span_id []byte) (time.Time, []*tracepb.Span) {
	plan_map := plan.(map[string]interface{})

	var spans []*tracepb.Span

	var span tracepb.Span
	span.TraceId = trace_id
	span.ParentSpanId = parent_span_id
	span_id := generateSpanId()
	span.SpanId = span_id

	node_type := plan_map["Node Type"].(string)
	span.Name = &tracepb.TruncatableString{Value: node_type}

	// Note that actual start time is the time when all the children has returned and this plan is ready to work.
	// It is different with the google's way of a span start time.
	start_offset := plan_map["Actual Startup Time"].(float64)
	start_offset_us := int64(start_offset * 1000)
	span_start_time := trace_start_time.Add(time.Duration(start_offset_us) * time.Microsecond)
	if plans := plan_map["Plans"]; plans != nil {
		for _, child_plan := range plans.([]interface{}) {
			child_span_start_time, child_spans := parseChildPlan(child_plan, trace_start_time, trace_id, span_id)
			if span_start_time.After(child_span_start_time) {
				span_start_time = child_span_start_time
			}
			spans = append(spans, child_spans...)
		}
	}
	span.StartTime = internal.TimeToTimestamp(span_start_time)

	end_offset := plan_map["Actual Total Time"].(float64)
	end_offset_us := int64(end_offset * 1000)
	span_end_time := trace_start_time.Add(time.Duration(end_offset_us) * time.Microsecond)
	span.EndTime = internal.TimeToTimestamp(span_end_time)

	attributes := make(map[string]*tracepb.AttributeValue)
	rows := plan_map["Actual Rows"].(float64)
	attributes["Rows Fetched"] = int64ToAttributeValue(int64(rows))

	if operation := plan_map["Operation"]; operation != nil {
		attributes["Operation"] = stringToAttributeValue(operation.(string))
	}

	if table := plan_map["Relation Name"]; table != nil {
		attributes["Table Name"] = stringToAttributeValue(table.(string))
	}
	span.Attributes = &tracepb.Span_Attributes{AttributeMap: attributes}

	spans = append(spans, &span)

	return span_start_time, spans
}
