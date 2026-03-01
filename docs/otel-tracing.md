# OpenTelemetry Tracing with Running Man

Running Man now includes built-in OpenTelemetry tracing support, making it easy to add distributed tracing to your local development workflow.

## Overview

Running Man provides:
- **OTLP HTTP receiver** on port 4318 (configurable)
- **Automatic environment variable injection** for managed processes
- **In-memory span storage** with configurable retention
- **Trace-log correlation** (coming soon)

## Quick Start

### 1. Enable Tracing

Tracing is enabled by default. To verify, run:

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

- `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318`
- `OTEL_SERVICE_NAME=<process_name>`
- `OTEL_PROPAGATORS=tracecontext,baggage`
- Plus recommended defaults for local development

## Python Setup Examples

### Basic Python Application

Here's a minimal Python application with OpenTelemetry instrumentation:

**requirements.txt:**
```txt
opentelemetry-api==1.28.0
opentelemetry-sdk==1.28.0
opentelemetry-exporter-otlp==1.28.0
opentelemetry-instrumentation==0.51b0
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

For web applications, use OpenTelemetry instrumentation:

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

**running-man.yml:**
```yaml
processes:
  - name: flask-api
    command: python flask_app.py
    # Auto-instrumentation will capture all requests automatically
```

### Django Application

For Django applications, use Django instrumentation:

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

**running-man.yml:**
```yaml
processes:
  - name: django-app
    command: python manage.py runserver
    # Django will be auto-instrumented
```

## Configuration Options

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

## Environment Variables Injected

Running Man injects these environment variables into managed processes:

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

## Troubleshooting

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

## Advanced Usage

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

## Next Steps

- **Trace visualization**: View traces in the Running Man TUI (coming soon)
- **Trace-log correlation**: Correlate traces with log entries
- **Custom exporters**: Export traces to Jaeger, Zipkin, etc.
- **Metrics support**: Add OpenTelemetry metrics collection

## Resources

- [OpenTelemetry Python Documentation](https://opentelemetry.io/docs/instrumentation/python/)
- [OpenTelemetry Python GitHub](https://github.com/open-telemetry/opentelemetry-python)
- [Running Man GitHub](https://github.com/elbeanio/the_running_man)