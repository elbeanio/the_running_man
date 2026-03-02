package tracing

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

// SpanEntry represents a stored trace span
type SpanEntry struct {
	TraceID      string            `json:"TraceID"`
	SpanID       string            `json:"SpanID"`
	ParentSpanID string            `json:"ParentSpanID"`
	Name         string            `json:"Name"`
	Kind         string            `json:"Kind"`
	StartTime    time.Time         `json:"StartTime"`
	EndTime      time.Time         `json:"EndTime"`
	Duration     time.Duration     `json:"Duration"`
	Status       string            `json:"Status"`
	StatusCode   string            `json:"StatusCode"`
	ServiceName  string            `json:"ServiceName"`
	Attributes   map[string]string `json:"Attributes"`
	Events       []SpanEvent       `json:"Events"`
	Links        []SpanLink        `json:"Links"`
}

// MarshalJSON implements custom JSON marshaling for SpanEntry
func (s SpanEntry) MarshalJSON() ([]byte, error) {
	type Alias SpanEntry
	return json.Marshal(&struct {
		Duration string `json:"Duration"`
		Alias
	}{
		Duration: s.Duration.String(),
		Alias:    (Alias)(s),
	})
}

// SpanEvent represents an event within a span
type SpanEvent struct {
	Name       string            `json:"Name"`
	Timestamp  time.Time         `json:"Timestamp"`
	Attributes map[string]string `json:"Attributes"`
}

// SpanLink represents a link to another span
type SpanLink struct {
	TraceID    string            `json:"TraceID"`
	SpanID     string            `json:"SpanID"`
	Attributes map[string]string `json:"Attributes"`
}

// convertOTLPSpan converts an OTLP span to our internal format
func convertOTLPSpan(span *tracev1.Span, resource *resourcev1.Resource) *SpanEntry {
	// Extract service name from resource attributes
	serviceName := extractServiceName(resource)

	// Convert trace and span IDs
	traceID := bytesToHex(span.TraceId)
	spanID := bytesToHex(span.SpanId)

	var parentSpanID string
	if len(span.ParentSpanId) > 0 {
		parentSpanID = bytesToHex(span.ParentSpanId)
	}

	// Convert timestamps
	startTime := timestampToTime(span.StartTimeUnixNano)
	endTime := timestampToTime(span.EndTimeUnixNano)
	duration := endTime.Sub(startTime)

	// Determine status
	status := "ok"
	statusCode := "STATUS_CODE_UNSET"
	if span.Status != nil {
		statusCode = span.Status.Code.String()
		if span.Status.Code == tracev1.Status_STATUS_CODE_ERROR {
			status = "error"
		} else if span.Status.Code == tracev1.Status_STATUS_CODE_OK {
			status = "ok"
		} else {
			status = "unset"
		}
	}

	// Convert span kind
	kind := span.Kind.String()

	// Extract attributes
	attributes := extractAttributes(span.Attributes)

	// Extract events
	events := make([]SpanEvent, len(span.Events))
	for i, event := range span.Events {
		events[i] = SpanEvent{
			Name:       event.Name,
			Timestamp:  timestampToTime(event.TimeUnixNano),
			Attributes: extractAttributes(event.Attributes),
		}
	}

	// Extract links
	links := make([]SpanLink, len(span.Links))
	for i, link := range span.Links {
		links[i] = SpanLink{
			TraceID:    bytesToHex(link.TraceId),
			SpanID:     bytesToHex(link.SpanId),
			Attributes: extractAttributes(link.Attributes),
		}
	}

	return &SpanEntry{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Name:         span.Name,
		Kind:         kind,
		StartTime:    startTime,
		EndTime:      endTime,
		Duration:     duration,
		Status:       status,
		StatusCode:   statusCode,
		ServiceName:  serviceName,
		Attributes:   attributes,
		Events:       events,
		Links:        links,
	}
}

// extractServiceName extracts service.name from resource attributes
func extractServiceName(resource *resourcev1.Resource) string {
	if resource == nil {
		return "unknown"
	}

	for _, attr := range resource.Attributes {
		if attr.Key == "service.name" {
			if strVal := attr.Value.GetStringValue(); strVal != "" {
				return strVal
			}
		}
	}

	return "unknown"
}

// extractAttributes converts OTLP attributes to a string map
func extractAttributes(attrs []*commonv1.KeyValue) map[string]string {
	result := make(map[string]string)
	for _, attr := range attrs {
		key := attr.Key
		value := attributeValueToString(attr.Value)
		if value != "" {
			result[key] = value
		}
	}
	return result
}

// attributeValueToString converts an OTLP AnyValue to string
func attributeValueToString(value *commonv1.AnyValue) string {
	switch v := value.Value.(type) {
	case *commonv1.AnyValue_StringValue:
		return v.StringValue
	case *commonv1.AnyValue_IntValue:
		return fmt.Sprintf("%d", v.IntValue)
	case *commonv1.AnyValue_DoubleValue:
		return fmt.Sprintf("%f", v.DoubleValue)
	case *commonv1.AnyValue_BoolValue:
		return fmt.Sprintf("%t", v.BoolValue)
	case *commonv1.AnyValue_ArrayValue:
		// For arrays, return a JSON-like representation
		items := v.ArrayValue.Values
		strs := make([]string, len(items))
		for i, item := range items {
			strs[i] = attributeValueToString(item)
		}
		return fmt.Sprintf("[%s]", strings.Join(strs, ", "))
	case *commonv1.AnyValue_KvlistValue:
		// For key-value lists, return a simple representation
		return "{...}"
	case *commonv1.AnyValue_BytesValue:
		return fmt.Sprintf("<bytes:%d>", len(v.BytesValue))
	default:
		return ""
	}
}

// bytesToHex converts byte slice to hex string
func bytesToHex(bytes []byte) string {
	if len(bytes) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", bytes)
}

// timestampToTime converts OTLP timestamp (nanoseconds) to time.Time
func timestampToTime(nanos uint64) time.Time {
	if nanos == 0 {
		return time.Time{}
	}
	seconds := int64(nanos / 1_000_000_000)
	nanosRemainder := int64(nanos % 1_000_000_000)
	return time.Unix(seconds, nanosRemainder)
}
