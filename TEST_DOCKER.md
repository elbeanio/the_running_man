# Docker Compose Integration Testing

This document describes how to test the Docker Compose integration functionality.

## Prerequisites

- Docker installed and running
- Docker Compose (or `docker compose` plugin)

## Test Setup

The `test-docker-compose.yml` file contains three simple Alpine Linux containers that generate test logs:

- **logger-1**: Logs a timestamp message every second
- **logger-2**: Logs an incrementing counter every 2 seconds
- **error-logger**: Logs to both stdout and stderr every 3 seconds

## Manual Testing

### 1. Start the test containers

```bash
docker-compose -f test-docker-compose.yml up -d
```

Verify containers are running:
```bash
docker-compose -f test-docker-compose.yml ps
```

### 2. Run The Running Man with Docker Compose

```bash
./running-man run --docker-compose test-docker-compose.yml
```

You should see:
- Container discovery output showing 3 containers found
- Log output from all three containers with `[container-name]` prefixes
- Mixed stdout/stderr streams (error-logger generates both)

### 3. Test with mixed sources (Docker + wrapped processes)

```bash
./running-man run --docker-compose test-docker-compose.yml --wrap "echo 'Hello from process'"
```

This tests running both Docker containers and regular processes simultaneously.

### 4. Query the API

While The Running Man is running (in another terminal):

```bash
# Get all logs
curl http://localhost:9000/logs

# Get recent logs (last 10 seconds)
curl http://localhost:9000/logs?since=10s

# Get logs from specific source
curl http://localhost:9000/logs?source=logger-1

# Get error logs
curl http://localhost:9000/errors

# Health check
curl http://localhost:9000/health
```

### 5. Cleanup

Stop The Running Man (Ctrl+C), then stop the containers:

```bash
docker-compose -f test-docker-compose.yml down
```

## Expected Behavior

- **Container Discovery**: All 3 containers should be discovered automatically
- **Log Streaming**: Logs from all containers appear in real-time with prefixes
- **Stderr Handling**: Error messages from error-logger appear (may show in red on terminal)
- **API Integration**: All container logs queryable via API
- **Graceful Shutdown**: Containers keep running after The Running Man exits

## Troubleshooting

### "No running containers found"

Make sure containers are running:
```bash
docker-compose -f test-docker-compose.yml ps
```

If not running, start them:
```bash
docker-compose -f test-docker-compose.yml up -d
```

### "Docker daemon not available"

Make sure Docker is running:
```bash
docker ps
```

### Container names don't match

The Running Man uses Docker Compose labels to discover containers. If you manually started containers, they may not have the correct labels. Always use `docker-compose up`.

## Integration Test Script

The `test_phase2.sh` script includes automated Docker integration tests that:

1. Check if Docker is available (skip if not)
2. Start test containers
3. Run The Running Man with Docker Compose
4. Verify log capture from containers
5. Clean up containers

Run with:
```bash
./test_phase2.sh
```
