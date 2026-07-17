package tracing

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestReceiver_StartStop(t *testing.T) {
	storage := NewSpanStorage(100, time.Hour)
	receiver := NewReceiver(storage, 0) // Port 0 for random port

	// Start receiver
	err := receiver.Start()
	require.NoError(t, err)
	defer receiver.Stop(context.Background())

	// Receiver should be started
	assert.True(t, receiver.started)
}

func TestReceiver_HealthEndpoint(t *testing.T) {
	storage := NewSpanStorage(100, time.Hour)
	receiver := NewReceiver(storage, 0)

	err := receiver.Start()
	require.NoError(t, err)
	defer receiver.Stop(context.Background())

	// Get the actual port (we need to know it)
	// Since we used port 0, we can't easily get the actual port
	// For now, skip this test or mock it
	t.Skip("Need to get actual port from receiver")
}

func TestReceiver_HandleTraces_Protobuf(t *testing.T) {
	storage := NewSpanStorage(100, time.Hour)
	receiver := NewReceiver(storage, 0)

	err := receiver.Start()
	require.NoError(t, err)
	defer receiver.Stop(context.Background())

	// Create a simple trace request
	traceRequest := &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{
						{
							Key: "service.name",
							Value: &commonv1.AnyValue{
								Value: &commonv1.AnyValue_StringValue{
									StringValue: "test-service",
								},
							},
						},
					},
				},
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								TraceId:           []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
								SpanId:            []byte{1, 2, 3, 4, 5, 6, 7, 8},
								Name:              "test-operation",
								Kind:              tracev1.Span_SPAN_KIND_SERVER,
								StartTimeUnixNano: uint64(time.Now().UnixNano()),
								EndTimeUnixNano:   uint64(time.Now().Add(100 * time.Millisecond).UnixNano()),
								Status: &tracev1.Status{
									Code: tracev1.Status_STATUS_CODE_OK,
								},
							},
						},
					},
				},
			},
		},
	}

	// Marshal to protobuf
	_, err = proto.Marshal(traceRequest)
	require.NoError(t, err)

	// Send request (we need the actual port, so skip for now)
	t.Skip("Need receiver port to send HTTP request")
}

func TestReceiver_HandleTraces_JSON(t *testing.T) {
	storage := NewSpanStorage(100, time.Hour)
	receiver := NewReceiver(storage, 0)

	err := receiver.Start()
	require.NoError(t, err)
	defer receiver.Stop(context.Background())

	// Create a simple trace request
	traceRequest := &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{
						{
							Key: "service.name",
							Value: &commonv1.AnyValue{
								Value: &commonv1.AnyValue_StringValue{
									StringValue: "test-service",
								},
							},
						},
					},
				},
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								TraceId:           []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
								SpanId:            []byte{1, 2, 3, 4, 5, 6, 7, 8},
								Name:              "test-operation",
								Kind:              tracev1.Span_SPAN_KIND_SERVER,
								StartTimeUnixNano: uint64(time.Now().UnixNano()),
								EndTimeUnixNano:   uint64(time.Now().Add(100 * time.Millisecond).UnixNano()),
								Status: &tracev1.Status{
									Code: tracev1.Status_STATUS_CODE_OK,
								},
							},
						},
					},
				},
			},
		},
	}

	// Marshal to JSON
	marshaler := protojson.MarshalOptions{
		UseProtoNames: true,
	}
	_, err = marshaler.Marshal(traceRequest)
	require.NoError(t, err)

	// Send request (we need the actual port, so skip for now)
	t.Skip("Need receiver port to send HTTP request")
}

func TestReceiver_ProcessTraceRequest(t *testing.T) {
	storage := NewSpanStorage(100, time.Hour)
	receiver := NewReceiver(storage, 0)

	// Create a trace request with multiple spans
	traceRequest := &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{
						{
							Key: "service.name",
							Value: &commonv1.AnyValue{
								Value: &commonv1.AnyValue_StringValue{
									StringValue: "test-service",
								},
							},
						},
					},
				},
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								TraceId:           []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
								SpanId:            []byte{1, 2, 3, 4, 5, 6, 7, 8},
								Name:              "operation-1",
								Kind:              tracev1.Span_SPAN_KIND_SERVER,
								StartTimeUnixNano: uint64(time.Now().UnixNano()),
								EndTimeUnixNano:   uint64(time.Now().Add(100 * time.Millisecond).UnixNano()),
							},
							{
								TraceId:           []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
								SpanId:            []byte{2, 3, 4, 5, 6, 7, 8, 9},
								ParentSpanId:      []byte{1, 2, 3, 4, 5, 6, 7, 8},
								Name:              "operation-2",
								Kind:              tracev1.Span_SPAN_KIND_CLIENT,
								StartTimeUnixNano: uint64(time.Now().Add(10 * time.Millisecond).UnixNano()),
								EndTimeUnixNano:   uint64(time.Now().Add(50 * time.Millisecond).UnixNano()),
							},
						},
					},
				},
			},
		},
	}

	// Process the request
	spansProcessed := receiver.processTraceRequest(traceRequest)
	assert.Equal(t, 2, spansProcessed)

	// Check that spans were stored
	spans := storage.Query(SpanQueryFilters{})
	assert.Len(t, spans, 2)

	// Verify span data
	assert.Equal(t, "0102030405060708090a0b0c0d0e0f10", spans[0].TraceID)
	assert.Equal(t, "0102030405060708", spans[0].SpanID)
	assert.Equal(t, "operation-1", spans[0].Name)
	assert.Equal(t, "test-service", spans[0].ServiceName)

	assert.Equal(t, "0102030405060708090a0b0c0d0e0f10", spans[1].TraceID)
	assert.Equal(t, "0203040506070809", spans[1].SpanID)
	assert.Equal(t, "0102030405060708", spans[1].ParentSpanID)
	assert.Equal(t, "operation-2", spans[1].Name)
}

func TestReceiver_UnsupportedContentType(t *testing.T) {
	// This test would require mocking HTTP requests
	// For now, just verify the function handles unsupported types
	t.Skip("Need HTTP mocking for this test")
}

func TestReceiver_MethodNotAllowed(t *testing.T) {
	// This test would require mocking HTTP requests
	// For now, just verify the function handles wrong methods
	t.Skip("Need HTTP mocking for this test")
}

// Test helper functions
func TestConvertOTLPSpan(t *testing.T) {
	now := time.Now()
	span := &tracev1.Span{
		TraceId:           []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanId:            []byte{1, 2, 3, 4, 5, 6, 7, 8},
		ParentSpanId:      []byte{0, 1, 2, 3, 4, 5, 6, 7},
		Name:              "test-operation",
		Kind:              tracev1.Span_SPAN_KIND_SERVER,
		StartTimeUnixNano: uint64(now.UnixNano()),
		EndTimeUnixNano:   uint64(now.Add(100 * time.Millisecond).UnixNano()),
		Status: &tracev1.Status{
			Code: tracev1.Status_STATUS_CODE_OK,
		},
		Attributes: []*commonv1.KeyValue{
			{
				Key: "http.method",
				Value: &commonv1.AnyValue{
					Value: &commonv1.AnyValue_StringValue{
						StringValue: "GET",
					},
				},
			},
			{
				Key: "http.status_code",
				Value: &commonv1.AnyValue{
					Value: &commonv1.AnyValue_IntValue{
						IntValue: 200,
					},
				},
			},
		},
	}

	resource := &resourcev1.Resource{
		Attributes: []*commonv1.KeyValue{
			{
				Key: "service.name",
				Value: &commonv1.AnyValue{
					Value: &commonv1.AnyValue_StringValue{
						StringValue: "test-service",
					},
				},
			},
		},
	}

	result := convertOTLPSpan(span, resource)

	assert.Equal(t, "0102030405060708090a0b0c0d0e0f10", result.TraceID)
	assert.Equal(t, "0102030405060708", result.SpanID)
	assert.Equal(t, "0001020304050607", result.ParentSpanID)
	assert.Equal(t, "test-operation", result.Name)
	assert.Equal(t, "test-service", result.ServiceName)
	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, "STATUS_CODE_OK", result.StatusCode)
	assert.Equal(t, "SPAN_KIND_SERVER", result.Kind)
	assert.Equal(t, "GET", result.Attributes["http.method"])
	assert.Equal(t, "200", result.Attributes["http.status_code"])
	assert.InDelta(t, 100*time.Millisecond, result.Duration, float64(1*time.Millisecond))
}

func TestExtractServiceName(t *testing.T) {
	// Test with service.name attribute
	resource := &resourcev1.Resource{
		Attributes: []*commonv1.KeyValue{
			{
				Key: "service.name",
				Value: &commonv1.AnyValue{
					Value: &commonv1.AnyValue_StringValue{
						StringValue: "my-service",
					},
				},
			},
		},
	}

	assert.Equal(t, "my-service", extractServiceName(resource))

	// Test without service.name
	resource2 := &resourcev1.Resource{
		Attributes: []*commonv1.KeyValue{
			{
				Key: "other.attribute",
				Value: &commonv1.AnyValue{
					Value: &commonv1.AnyValue_StringValue{
						StringValue: "value",
					},
				},
			},
		},
	}

	assert.Equal(t, "unknown", extractServiceName(resource2))

	// Test nil resource
	assert.Equal(t, "unknown", extractServiceName(nil))
}

func TestAttributeValueToString(t *testing.T) {
	// Test string value
	strVal := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_StringValue{
			StringValue: "test",
		},
	}
	assert.Equal(t, "test", attributeValueToString(strVal))

	// Test int value
	intVal := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_IntValue{
			IntValue: 42,
		},
	}
	assert.Equal(t, "42", attributeValueToString(intVal))

	// Test bool value
	boolVal := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_BoolValue{
			BoolValue: true,
		},
	}
	assert.Equal(t, "true", attributeValueToString(boolVal))

	// Test double value
	doubleVal := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_DoubleValue{
			DoubleValue: 3.14,
		},
	}
	assert.Equal(t, "3.140000", attributeValueToString(doubleVal))

	// Test bytes value
	bytesVal := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_BytesValue{
			BytesValue: []byte{1, 2, 3},
		},
	}
	assert.Equal(t, "<bytes:3>", attributeValueToString(bytesVal))

	// Test array value
	arrayVal := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_ArrayValue{
			ArrayValue: &commonv1.ArrayValue{
				Values: []*commonv1.AnyValue{
					{Value: &commonv1.AnyValue_StringValue{StringValue: "a"}},
					{Value: &commonv1.AnyValue_IntValue{IntValue: 1}},
				},
			},
		},
	}
	assert.Equal(t, "[a, 1]", attributeValueToString(arrayVal))

	// Test kvlist value
	kvlistVal := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_KvlistValue{
			KvlistValue: &commonv1.KeyValueList{
				Values: []*commonv1.KeyValue{},
			},
		},
	}
	assert.Equal(t, "{...}", attributeValueToString(kvlistVal))
}

// spansJSON builds a minimal OTLP/JSON body with the given trace/span id strings.
func spansJSON(traceID, spanID string) string {
	return fmt.Sprintf(
		`{"resourceSpans":[{"scopeSpans":[{"spans":[`+
			`{"traceId":%q,"spanId":%q,"name":"n","kind":1}]}]}]}`, traceID, spanID)
}

// TestFixHexTraceIDs_HexRoundTrips documents the OTLP/JSON hex-id bug and its fix:
// a conformant exporter sends hex ids, protojson base64-decodes them into garbage,
// and fixHexTraceIDs recovers the correct bytes. Uses the plan §10 reproducer ids.
func TestFixHexTraceIDs_HexRoundTrips(t *testing.T) {
	const hexTrace = "5b8aa5a2d2c872e8321cf37308d69df2" // 32 hex chars → 16 bytes
	const hexSpan = "051581bf3cb55c13"                  // 16 hex chars → 8 bytes
	wantTrace, err := hex.DecodeString(hexTrace)
	require.NoError(t, err)
	wantSpan, err := hex.DecodeString(hexSpan)
	require.NoError(t, err)

	var req collectortracev1.ExportTraceServiceRequest
	require.NoError(t, protojson.Unmarshal([]byte(spansJSON(hexTrace, hexSpan)), &req))

	// Bug: protojson decoded the hex as base64, so the ids are the wrong length.
	got := req.ResourceSpans[0].ScopeSpans[0].Spans[0]
	require.Len(t, got.TraceId, 24, "protojson mis-decodes 32 hex chars into 24 bytes")
	require.NotEqual(t, wantTrace, got.TraceId)

	fixHexTraceIDs(&req)

	got = req.ResourceSpans[0].ScopeSpans[0].Spans[0]
	assert.Equal(t, wantTrace, got.TraceId)
	assert.Equal(t, wantSpan, got.SpanId)
	assert.Equal(t, hexTrace, fmt.Sprintf("%x", got.TraceId))
	assert.Equal(t, hexSpan, fmt.Sprintf("%x", got.SpanId))
}

// TestFixHexTraceIDs_Base64Unchanged proves backward compatibility: a client that
// sends genuine base64 ids (already the right byte length) is left untouched.
func TestFixHexTraceIDs_Base64Unchanged(t *testing.T) {
	rawTrace := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	rawSpan := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	body := spansJSON(
		base64.StdEncoding.EncodeToString(rawTrace),
		base64.StdEncoding.EncodeToString(rawSpan),
	)

	var req collectortracev1.ExportTraceServiceRequest
	require.NoError(t, protojson.Unmarshal([]byte(body), &req))
	fixHexTraceIDs(&req)

	got := req.ResourceSpans[0].ScopeSpans[0].Spans[0]
	assert.Equal(t, rawTrace, got.TraceId)
	assert.Equal(t, rawSpan, got.SpanId)
}

// TestHexDecodeID_Edges covers the empty (absent parent) and already-correct cases.
func TestHexDecodeID_Edges(t *testing.T) {
	assert.Empty(t, hexDecodeID(nil, 8))
	assert.Empty(t, hexDecodeID([]byte{}, 8))
	correct := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	assert.Equal(t, correct, hexDecodeID(correct, 8))
}
