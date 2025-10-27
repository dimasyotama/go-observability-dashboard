# Go Observability Dashboard

This project demonstrates a comprehensive observability setup for a Go web application using a modern, open-source stack. It integrates metrics, logs, and traces into a unified Grafana dashboard, providing deep insights into application performance and behavior.

The entire stack is containerized using Docker Compose for easy setup and teardown.

![Grafana Dashboard Screenshot](https://raw.githubusercontent.com/dimasyoga/go-observability-dashboard/main/assets/go-dashboard.png)

## Features

- **Go Application**: A simple Gin-based web application with several API endpoints.
- **Metrics**: Prometheus scrapes custom application metrics and Go runtime metrics.
- **Logging**: Vector collects logs from the application container and forwards them to Loki.
- **Tracing**: OpenTelemetry is used to instrument the Go application, sending traces to Tempo via the OTel Collector.
- **Visualization**: Grafana comes pre-configured with datasources for Prometheus, Loki, and Tempo, and includes a custom dashboard to visualize all the telemetry data.
- **Load Testing**: A k6 script is included to generate traffic and simulate real-world load on the application.

## Architecture

The components work together as follows:

1.  **k6 (Load Generator)**: Sends HTTP requests to the `the-app` service.
2.  **the-app (Go Application)**:
    - Serves API requests.
    - Exposes an HTTP endpoint (`/metrics`) for Prometheus to scrape.
    - Sends trace data via OTLP to the `otel-collector`.
    - Writes structured JSON logs to `stdout`.
3.  **Vector**:
    - Listens to the Docker socket for logs from the `the-app` container.
    - Parses the JSON logs, adds metadata (like `service_name`), and sends them to `Loki`.
4.  **Prometheus**:
    - Scrapes metrics from `the-app`, `loki`, `tempo`, and `vector`.
5.  **OTel Collector**:
    - Receives traces from `the-app`.
    - Batches the traces and forwards them to `Tempo`.
6.  **Loki**:
    - Receives and stores log streams from `Vector`.
7.  **Tempo**:
    - Receives and stores traces from the `otel-collector`.
8.  **Grafana**:
    - Queries **Prometheus** for metrics.
    - Queries **Loki** for logs.
    - Queries **Tempo** for traces.
    - Correlates logs and traces, allowing you to jump from a log line to its corresponding trace.

```
                               +-------------------+
                          ---> |    Prometheus     |
                         /     +-------------------+
                        /              ^
                       /               |
+----------------+   /       +-------------------+
|      k6        |  /   /--->|   OTel Collector  | ---> +-------+
+----------------+   |  /     +-------------------+      | Tempo |
       |            | /                                +-------+
       |            |/                                     ^
       v            v                                      |
+----------------+   |      +-------------------+      +-------+
|    the-app     | --+----->|      Vector       |----->|  Loki |
+----------------+          +-------------------+      +-------+
 (Docker Logs)        (Metrics, Logs, Traces)           ^
       |                                                |
       +------------------------------------------------+
                               |
                               v
                       +---------------+
                       |    Grafana    |
                       +---------------+
```

## Prerequisites

- Docker
- Docker Compose
- k6 (for load testing)

## Getting Started

1.  **Clone the repository:**
    ```sh
    git clone https://github.com/dimasyoga/go-observability-dashboard.git
    cd go-observability-dashboard
    ```

2.  **Create the external Docker network:**
    The `docker-compose.yaml` is configured to use an external network named `monitoring`. Create it first:
    ```sh
    docker network create monitoring
    ```

3.  **Start the services:**
    Use Docker Compose to build the Go app image and start all services in the background.
    ```sh
    docker-compose up -d --build
    ```

4.  **Verify the services are running:**
    Check the status of the containers. It may take a minute for all health checks to pass.
    ```sh
    docker-compose ps
    ```

## Accessing the Services

- **Go Application**: `http://localhost:5060/status`
- **Grafana**: `http://localhost:3000`
  - **Login**: `admin` / `admin`
  - The "Go Application Dashboard" will be available under Dashboards.
- **Prometheus**: `http://localhost:9090`
- **Tempo**: `http://localhost:3200`

## Generating Load and Viewing Data

1.  **Run the k6 stress test:**
    Open a new terminal and run the k6 script. This will generate traffic to the Go application's endpoints for a few minutes.
    ```sh
    k6 run stress_test.js
    ```

2.  **Explore the Grafana Dashboard:**
    - Navigate to Grafana.
    - Open the **Go Application Dashboard**.
    - Set the refresh interval in the top-right corner to `5s`.

    You will see panels for:
    - Total requests, request rates, and error rates.
    - Request latency percentiles (p60, p90).
    - Application CPU and memory usage.
    - A live stream of application logs from Loki.
    - In the logs panel, you can click the button next to a `trace_id` to jump directly to the trace in Tempo.

## Stopping the Stack

To stop and remove all the containers, run:
```sh
docker-compose down -v
```
The `-v` flag also removes the Docker volumes, deleting all stored metrics, logs, and traces.