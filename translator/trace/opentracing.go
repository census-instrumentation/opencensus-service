package tracetranslator

// TODO: (@pjanotti): Replace any OpenTracing literals by importing github.com/opentracing/opentracing-go/ext?
// Opentracing constants that indicate special attributes
const (
	OpentracingKeySpanKind          = "span.kind"
	OpentracingKeyHTTPStatusCode    = "http.status_code"
	OpentracingKeyStatusCode        = "status_code"
	OpentracingKeyHTTPStatusMessage = "http.status_message"
	OpentracingKeyStatusMessage     = "status_message"
	OpentracingKeyMessage           = "message"
)
