package tracing

import "strings"

// ProcessEnv returns the environment for a process Running Man starts: the
// inherited environment with any OTEL_* variables removed, and Running Man's
// added after it.
//
// There used to be two implementations of this. The one here, OTELEnvVars, was
// tested and never called. The one that ran, inline in the process wrapper, had
// no tests. They differed on the point that matters: this one prepended its
// variables without removing inherited ones, under a comment saying that made
// them "take precedence" -- but os/exec keeps the LAST value of a duplicated
// key, so the inherited value would have won. The wrapper removed inherited
// ones first, which is what actually guarantees precedence. That behaviour is
// kept here, with the variables appended so the guarantee would hold even
// without the filter, and in a fixed order rather than a map's.
func ProcessEnv(inherited []string, endpoint, serviceName string) []string {
	env := make([]string, 0, len(inherited)+8)
	for _, e := range inherited {
		if !strings.HasPrefix(e, "OTEL_") {
			env = append(env, e)
		}
	}
	return append(env,
		"OTEL_EXPORTER_OTLP_ENDPOINT="+endpoint,
		"OTEL_SERVICE_NAME="+serviceName,
		"OTEL_PROPAGATORS=tracecontext,baggage",
		"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
		"OTEL_RESOURCE_ATTRIBUTES=deployment.environment=local",
		"OTEL_TRACES_SAMPLER=always_on",
		"OTEL_METRICS_SAMPLER=always_on",
		"OTEL_LOGS_SAMPLER=always_on",
	)
}
