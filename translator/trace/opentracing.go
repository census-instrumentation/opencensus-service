package tracetranslator

// TODO: (@pjanotti): Replace any OpenTracing literals by importing github.com/opentracing/opentracing-go/ext?
// Opentracing constants that indicate special attributes
const (
	OpentracingKey_SPAN_KIND           = "span.kind"
	OpentracingKey_HTTP_STATUS_CODE    = "http.status_code"
	OpentracingKey_STATUS_CODE         = "status_code"
	OpentracingKey_HTTP_STATUS_MESSAGE = "http.status_message"
	OpentracingKey_STATUS_MESSAGE      = "status_message"
	OpentracingKey_MESSAGE             = "message"
)
