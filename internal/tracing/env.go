package tracing

import (
	"fmt"
	"os"
	"strings"
)

// OTELEnvVars holds OpenTelemetry environment variable configuration
type OTELEnvVars struct {
	// OTLP endpoint URL (e.g., http://localhost:4318)
	Endpoint string

	// Service name for traces
	ServiceName string

	// Whether to inject environment variables
	Enabled bool
}

// NewOTELEnvVars creates OTEL environment variable configuration
func NewOTELEnvVars(endpoint string, serviceName string, enabled bool) *OTELEnvVars {
	return &OTELEnvVars{
		Endpoint:    endpoint,
		ServiceName: serviceName,
		Enabled:     enabled,
	}
}

// Inject adds OTEL environment variables to the given environment slice
// Returns a new slice with OTEL variables added
func (o *OTELEnvVars) Inject(env []string) []string {
	if !o.Enabled {
		return env
	}

	// Create a copy of the environment
	result := make([]string, len(env))
	copy(result, env)

	// Add OTEL environment variables
	otelVars := []string{
		fmt.Sprintf("OTEL_EXPORTER_OTLP_ENDPOINT=%s", o.Endpoint),
		fmt.Sprintf("OTEL_SERVICE_NAME=%s", o.ServiceName),
		"OTEL_PROPAGATORS=tracecontext,baggage",
		// Additional recommended defaults for local development
		"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
		"OTEL_RESOURCE_ATTRIBUTES=deployment.environment=local",
		"OTEL_TRACES_SAMPLER=always_on",
		"OTEL_METRICS_SAMPLER=always_on",
		"OTEL_LOGS_SAMPLER=always_on",
	}

	// Prepend OTEL vars (so they take precedence over any existing ones)
	result = append(otelVars, result...)

	return result
}

// InjectIntoProcess modifies the environment of the current process
// by adding OTEL environment variables
func (o *OTELEnvVars) InjectIntoProcess() {
	if !o.Enabled {
		return
	}

	// Set environment variables for current process
	os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", o.Endpoint)
	os.Setenv("OTEL_SERVICE_NAME", o.ServiceName)
	os.Setenv("OTEL_PROPAGATORS", "tracecontext,baggage")
	os.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	os.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=local")
	os.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	os.Setenv("OTEL_METRICS_SAMPLER", "always_on")
	os.Setenv("OTEL_LOGS_SAMPLER", "always_on")
}

// GetEnvVars returns OTEL environment variables as a map
func (o *OTELEnvVars) GetEnvVars() map[string]string {
	if !o.Enabled {
		return nil
	}

	return map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": o.Endpoint,
		"OTEL_SERVICE_NAME":           o.ServiceName,
		"OTEL_PROPAGATORS":            "tracecontext,baggage",
		"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
		"OTEL_RESOURCE_ATTRIBUTES":    "deployment.environment=local",
		"OTEL_TRACES_SAMPLER":         "always_on",
		"OTEL_METRICS_SAMPLER":        "always_on",
		"OTEL_LOGS_SAMPLER":           "always_on",
	}
}

// HasOTELEnvVars checks if the given environment contains OTEL variables
func HasOTELEnvVars(env []string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, "OTEL_") {
			return true
		}
	}
	return false
}

// FilterOTELEnvVars removes any existing OTEL environment variables
// Returns a new slice with OTEL variables removed
func FilterOTELEnvVars(env []string) []string {
	var result []string
	for _, e := range env {
		if !strings.HasPrefix(e, "OTEL_") {
			result = append(result, e)
		}
	}
	return result
}
