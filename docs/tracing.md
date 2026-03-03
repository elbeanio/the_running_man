# OpenTelemetry Tracing with Running Man

Running Man includes built-in OpenTelemetry tracing support, making it easy to add distributed tracing to your local development workflow.

## 📊 Overview

Running Man provides a complete OpenTelemetry solution for local development:

- **OTLP HTTP receiver** on port 4318 (configurable)
- **Automatic environment variable injection** for managed processes
- **In-memory span storage** with configurable retention
- **Trace-log correlation** via `trace_id`
- **MCP tools** for trace exploration via AI agents
- **REST API** for programmatic access to traces

## 🚀 Quick Start

### 1. Enable Tracing

Tracing is enabled by default. To verify:

```bash
running-man run --process "echo 'Hello'" --no-tui
```

You should see:
```
Tracing: OTLP receiver on http://localhost:4318
[tracing] OTLP receiver starting on http://localhost:4318
```

### 2. Configure Your Application

Running Man automatically injects OTEL environment variables into managed processes:

| Variable | Value | Purpose |
|----------|-------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | OTLP endpoint URL |
| `OTEL_SERVICE_NAME` | Process name from config | Service name for traces |
| `OTEL_PROPAGATORS` | `tracecontext,baggage` | Context propagation |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | OTLP protocol |
| `OTEL_RESOURCE_ATTRIBUTES` | `deployment.environment=local` | Resource attributes |
| `OTEL_TRACES_SAMPLER` | `always_on` | Sample all traces |
| `OTEL_METRICS_SAMPLER` | `always_on` | Sample all metrics |
| `OTEL_LOGS_SAMPLER` | `always_on` | Sample all logs |

## 🐍 Python Setup Examples

### Basic Python Application

**requirements.txt:**
```txt
opentelemetry-api==1.28.0
opentelemetry-sdk==1.28.0
opentelemetry-exporter-otlp==1.28.0
```

**app.py:**
```python
import time
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource

# Configure OpenTelemetry
resource = Resource.create({
    "service.name": "my-python-app",
    "deployment.environment": "local"
})

trace.set_tracer_provider(TracerProvider(resource=resource))
tracer_provider = trace.get_tracer_provider()

# OTLP exporter - will use OTEL_EXPORTER_OTLP_ENDPOINT from environment
otlp_exporter = OTLPSpanExporter()
span_processor = BatchSpanProcessor(otlp_exporter)
tracer_provider.add_span_processor(span_processor)

# Get a tracer
tracer = trace.get_tracer(__name__)

def process_order(order_id):
    """Example function that creates spans"""
    with tracer.start_as_current_span("process_order") as span:
        span.set_attribute("order.id", order_id)
        span.set_attribute("processing.stage", "started")

        # Simulate work
        time.sleep(0.1)

        with tracer.start_as_current_span("validate_order"):
            time.sleep(0.05)
            # Add event to span
            span.add_event("order.validated", {"order.id": order_id})

        with tracer.start_as_current_span("charge_payment"):
            time.sleep(0.08)
            span.set_attribute("payment.amount", 99.99)

        span.set_attribute("processing.stage", "completed")
        return f"Order {order_id} processed"

if __name__ == "__main__":
    # Process some orders
    for i in range(5):
        result = process_order(f"ORD-{1000 + i}")
        print(result)
        time.sleep(0.5)
```

**running-man.yml configuration:**
```yaml
processes:
  - name: python-app
    command: python app.py
    # OTEL environment variables will be automatically injected
    # OTEL_SERVICE_NAME will be set to "python-app"
```

### Flask Web Application

**requirements.txt:**
```txt
flask==3.0.0
opentelemetry-api==1.28.0
opentelemetry-sdk==1.28.0
opentelemetry-exporter-otlp==1.28.0
opentelemetry-instrumentation-flask==0.51b0
opentelemetry-instrumentation-requests==0.51b0
```

**flask_app.py:**
```python
from flask import Flask, jsonify
import requests
from opentelemetry import trace
from opentelemetry.instrumentation.flask import FlaskInstrumentor
from opentelemetry.instrumentation.requests import RequestsInstrumentor
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource

app = Flask(__name__)

# Auto-instrument Flask and Requests
FlaskInstrumentor().instrument_app(app)
RequestsInstrumentor().instrument()

# Configure OpenTelemetry (will use environment variables from Running Man)
resource = Resource.create({
    "service.name": "flask-api",
    "deployment.environment": "local"
})

trace.set_tracer_provider(TracerProvider(resource=resource))
tracer_provider = trace.get_tracer_provider()

# OTLP exporter - uses OTEL_EXPORTER_OTLP_ENDPOINT from environment
otlp_exporter = OTLPSpanExporter()
span_processor = BatchSpanProcessor(otlp_exporter)
tracer_provider.add_span_processor(span_processor)

tracer = trace.get_tracer(__name__)

@app.route('/')
def home():
    with tracer.start_as_current_span("home_endpoint"):
        return jsonify({"message": "Welcome to the Flask API"})

@app.route('/api/users')
def get_users():
    with tracer.start_as_current_span("get_users"):
        # Simulate database query
        with tracer.start_as_current_span("query_database"):
            users = [
                {"id": 1, "name": "Alice"},
                {"id": 2, "name": "Bob"},
                {"id": 3, "name": "Charlie"}
            ]

        # Make an external API call (auto-instrumented)
        with tracer.start_as_current_span("external_api_call"):
            response = requests.get('https://httpbin.org/delay/1', timeout=2)

        return jsonify({
            "users": users,
            "external_status": response.status_code
        })

@app.route('/api/orders/<int:order_id>')
def get_order(order_id):
    with tracer.start_as_current_span("get_order") as span:
        span.set_attribute("order.id", order_id)

        # Simulate processing
        with tracer.start_as_current_span("fetch_order_details"):
            order = {
                "id": order_id,
                "status": "processing",
                "amount": 99.99
            }

        return jsonify(order)

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000, debug=True)
```

### Django Application

**requirements.txt:**
```txt
django==4.2.0
opentelemetry-api==1.28.0
opentelemetry-sdk==1.28.0
opentelemetry-exporter-otlp==1.28.0
opentelemetry-instrumentation-django==0.51b0
```

**manage.py modification:**
```python
#!/usr/bin/env python
import os
import sys
from opentelemetry import trace
from opentelemetry.instrumentation.django import DjangoInstrumentor
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource

def main():
    """Run administrative tasks."""

    # Initialize OpenTelemetry before Django starts
    resource = Resource.create({
        "service.name": "django-app",
        "deployment.environment": "local"
    })

    trace.set_tracer_provider(TracerProvider(resource=resource))
    tracer_provider = trace.get_tracer_provider()

    # OTLP exporter - uses environment variables
    otlp_exporter = OTLPSpanExporter()
    span_processor = BatchSpanProcessor(otlp_exporter)
    tracer_provider.add_span_processor(span_processor)

    # Instrument Django
    DjangoInstrumentor().instrument()

    os.environ.setdefault('DJANGO_SETTINGS_MODULE', 'myproject.settings')

    try:
        from django.core.management import execute_from_command_line
    except ImportError as exc:
        raise ImportError(
            "Couldn't import Django. Are you sure it's installed?"
        ) from exc
    execute_from_command_line(sys.argv)

if __name__ == '__main__':
    main()
```

## ⚙️ Configuration

### YAML Configuration

```yaml
tracing:
  # Enable/disable tracing (default: true)
  enabled: true

  # OTLP receiver port (default: 4318)
  port: 4321  # Use if 4318 is occupied

  # Maximum spans to store (default: 10000)
  max_spans: 5000

  # How long to keep spans (default: 30m)
  max_span_age: 1h
```

### Command Line Flags

```bash
# Disable tracing
running-man run --process "python app.py" --tracing false

# Change tracing port
running-man run --process "python app.py" --tracing-port 4321

# Use custom config file
running-man run --config my-config.yml
```

## 🔍 Querying Traces

### REST API

```bash
# List recent traces
curl "http://localhost:9000/traces?since=5m"

# Get specific trace
curl "http://localhost:9000/traces/abc123-def456"

# Filter by service
curl "http://localhost:9000/traces?service_name=backend&since=10m"

# Find slow traces
curl "http://localhost:9000/traces?min_duration=1s&since=5m"

# Traces with errors
curl "http://localhost:9000/traces?status=error&since=30m"
```

### MCP Tools (AI Agent Integration)

Running Man provides MCP tools for trace exploration:

1. **`get_traces`** - List recent traces with filters
   - "Show me traces from the last 10 minutes"
   - "Find traces with errors from the backend service"
   - "Show me slow traces (longer than 1 second)"

2. **`get_trace`** - Get detailed trace information
   - "Show me details for trace abc123-def456"
   - "Get all spans for workflow XYZ"

3. **`get_slow_traces`** - Find traces exceeding duration thresholds
   - "Find traces slower than 500ms"
   - "Show me the slowest API endpoints"

### Example MCP Usage

```bash
# Start Running Man with tracing
running-man run --process "python app.py"

# AI agent can now:
# - "Show me recent traces with errors"
# - "Find the slowest database queries"
# - "Get trace details for failed user login"
# - "Show me traces from the payment service"
```

## 🎯 Advanced Usage

### Custom Span Attributes

Add custom attributes to spans for better filtering:

```python
from opentelemetry import trace

tracer = trace.get_tracer(__name__)

with tracer.start_as_current_span("api_request") as span:
    span.set_attribute("http.method", "GET")
    span.set_attribute("http.route", "/api/users")
    span.set_attribute("user.id", "12345")
    span.set_attribute("response.size_bytes", 2048)

    # Add events (timed annotations)
    span.add_event("cache.hit", {"key": "users:all"})

    # Set status
    span.set_status(trace.Status(trace.StatusCode.OK))
```

### Manual Instrumentation (Without Auto-injection)

If you need to manually set OTEL environment variables:

```python
import os
from opentelemetry import trace

# Manually set if not using Running Man's injection
os.environ.setdefault('OTEL_EXPORTER_OTLP_ENDPOINT', 'http://localhost:4318')
os.environ.setdefault('OTEL_SERVICE_NAME', 'my-manual-app')

# Rest of your OpenTelemetry setup...
```

### Trace-Log Correlation

Running Man automatically correlates logs with traces when logs contain `trace_id`:

```python
import logging
from opentelemetry import trace

logger = logging.getLogger(__name__)
tracer = trace.get_tracer(__name__)

def process_request(request_id):
    with tracer.start_as_current_span("process_request") as span:
        # Log with trace context
        logger.info(f"Processing request {request_id}", extra={
            "trace_id": span.get_span_context().trace_id,
            "span_id": span.get_span_context().span_id
        })
        
        # Your processing logic...
```

## 🚨 Troubleshooting

### Spans Not Appearing

1. **Check if tracing is enabled:**
   ```bash
   running-man run --process "env | grep OTEL" --no-tui
   ```
   Should show OTEL environment variables.

2. **Verify OTLP receiver is running:**
   Look for "Tracing: OTLP receiver on http://localhost:4318" in output.

3. **Check port conflicts:**
   If port 4318 is in use, change it:
   ```bash
   running-man run --process "python app.py" --tracing-port 4321
   ```

4. **Python package issues:**
   Ensure you have the correct packages:
   ```bash
   pip install opentelemetry-api opentelemetry-sdk opentelemetry-exporter-otlp
   ```

### Common Errors

**"Address already in use"**: Port 4318 is occupied. Use `--tracing-port` to specify a different port.

**"No spans received"**: Check that your application is properly instrumented and using the OTLP exporter.

**"Environment variables not set"**: Ensure tracing is enabled (default is true).

**"Trace storage full"**: Increase `max_spans` in configuration or reduce retention time.

### Performance Considerations

**Storage estimates:**
- Span size: ~0.5KB (limited attributes)
- 100 spans/minute: ~30KB/30min
- Default limit: 10,000 spans (~5MB)

**If you need more capacity:**
```yaml
tracing:
  max_spans: 50000  # Increase limit
  max_span_age: 1h  # Reduce retention
```

## 📚 Architecture

### Components

1. **OTEL Receiver** (`internal/tracing/receiver.go`)
   - OTLP HTTP receiver on port 4318
   - Supports JSON and Protobuf formats
   - Health endpoint for readiness checks

2. **Trace Storage** (`internal/tracing/storage.go`)
   - In-memory storage with configurable retention
   - Query capabilities by trace ID, service, status
   - Automatic correlation with logs

3. **Span Management** (`internal/tracing/span.go`)
   - Span data structure with full OTEL attributes
   - Parent-child relationship tracking
   - Duration calculation and status mapping

### Data Flow

```
Instrumented App → OTLP HTTP → Running Man Receiver → Trace Storage
                                                          ↓
                                                    REST API / MCP
                                                          ↓
                                                  Developer / AI Agent
```

## 🔮 Future Enhancements

Planned tracing improvements:

- **Trace visualization** in TUI
- **Export to Jaeger/Zipkin** for external analysis
- **Metrics collection** via OpenTelemetry
- **Custom span processors** for filtering/transformation
- **Distributed context propagation** across services

## 📖 Resources

- [OpenTelemetry Python Documentation](https://opentelemetry.io/docs/instrumentation/python/)
- [OpenTelemetry Python GitHub](https://github.com/open-telemetry/opentelemetry-python)
- [Running Man GitHub](https://github.com/elbeanio/the_running_man)
- [OpenTelemetry Specification](https://opentelemetry.io/docs/specs/)

---

**Need help?** Check the [Troubleshooting Guide](troubleshooting.md) or file an issue on [GitHub](https://github.com/elbeanio/the_running_man/issues).