package tracing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOTELEnvVars_Inject(t *testing.T) {
	// Test with enabled OTEL
	otel := NewOTELEnvVars("http://localhost:4318", "my-service", true)

	env := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/user",
		"EXISTING_VAR=value",
	}

	result := otel.Inject(env)

	// Should have added OTEL vars
	assert.Len(t, result, len(env)+8) // 8 OTEL vars added

	// Check OTEL vars are present
	assert.Contains(t, result, "OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318")
	assert.Contains(t, result, "OTEL_SERVICE_NAME=my-service")
	assert.Contains(t, result, "OTEL_PROPAGATORS=tracecontext,baggage")
	assert.Contains(t, result, "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf")

	// Original env vars should still be there
	assert.Contains(t, result, "PATH=/usr/bin:/bin")
	assert.Contains(t, result, "HOME=/home/user")
	assert.Contains(t, result, "EXISTING_VAR=value")

	// OTEL vars should come first (take precedence)
	assert.Equal(t, "OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318", result[0])
	assert.Equal(t, "OTEL_SERVICE_NAME=my-service", result[1])
}

func TestOTELEnvVars_Inject_Disabled(t *testing.T) {
	// Test with disabled OTEL
	otel := NewOTELEnvVars("http://localhost:4318", "my-service", false)

	env := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/user",
	}

	result := otel.Inject(env)

	// Should not add OTEL vars when disabled
	assert.Len(t, result, len(env))
	assert.Equal(t, env, result)
}

func TestOTELEnvVars_GetEnvVars(t *testing.T) {
	otel := NewOTELEnvVars("http://localhost:4318", "my-service", true)

	vars := otel.GetEnvVars()

	assert.Len(t, vars, 8)
	assert.Equal(t, "http://localhost:4318", vars["OTEL_EXPORTER_OTLP_ENDPOINT"])
	assert.Equal(t, "my-service", vars["OTEL_SERVICE_NAME"])
	assert.Equal(t, "tracecontext,baggage", vars["OTEL_PROPAGATORS"])
	assert.Equal(t, "http/protobuf", vars["OTEL_EXPORTER_OTLP_PROTOCOL"])
	assert.Equal(t, "deployment.environment=local", vars["OTEL_RESOURCE_ATTRIBUTES"])
	assert.Equal(t, "always_on", vars["OTEL_TRACES_SAMPLER"])
	assert.Equal(t, "always_on", vars["OTEL_METRICS_SAMPLER"])
	assert.Equal(t, "always_on", vars["OTEL_LOGS_SAMPLER"])
}

func TestOTELEnvVars_GetEnvVars_Disabled(t *testing.T) {
	otel := NewOTELEnvVars("http://localhost:4318", "my-service", false)

	vars := otel.GetEnvVars()

	assert.Nil(t, vars)
}

func TestHasOTELEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		env      []string
		expected bool
	}{
		{
			name: "Has OTEL vars",
			env: []string{
				"PATH=/usr/bin",
				"OTEL_SERVICE_NAME=test",
				"HOME=/home/user",
			},
			expected: true,
		},
		{
			name: "No OTEL vars",
			env: []string{
				"PATH=/usr/bin",
				"HOME=/home/user",
				"SOME_OTHER_VAR=value",
			},
			expected: false,
		},
		{
			name:     "Empty env",
			env:      []string{},
			expected: false,
		},
		{
			name: "OTEL-like but not OTEL",
			env: []string{
				"PATH=/usr/bin",
				"NOT_OTEL_VAR=value",
				"HOTEL_PRICE=100",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasOTELEnvVars(tt.env)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFilterOTELEnvVars(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"OTEL_SERVICE_NAME=test",
		"HOME=/home/user",
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318",
		"SOME_OTHER_VAR=value",
		"OTEL_PROPAGATORS=tracecontext,baggage",
	}

	result := FilterOTELEnvVars(env)

	// Should only have non-OTEL vars
	assert.Len(t, result, 3)
	assert.Contains(t, result, "PATH=/usr/bin")
	assert.Contains(t, result, "HOME=/home/user")
	assert.Contains(t, result, "SOME_OTHER_VAR=value")

	// Should not have OTEL vars
	for _, e := range result {
		assert.NotRegexp(t, `^OTEL_`, e)
	}
}

func TestFilterOTELEnvVars_NoOTEL(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/user",
		"SOME_OTHER_VAR=value",
	}

	result := FilterOTELEnvVars(env)

	// Should be unchanged
	assert.Equal(t, env, result)
}

func TestFilterOTELEnvVars_Empty(t *testing.T) {
	env := []string{}

	result := FilterOTELEnvVars(env)

	assert.Empty(t, result)
}
