package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"mini-cloud/internal/common/util"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type telemetry struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	requestCounter metric.Int64Counter
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx := context.Background()
	tel, shutdown, err := newTelemetry(ctx)
	if err != nil {
		logger.Error("initialize telemetry failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			logger.Warn("telemetry shutdown failed", "error", err)
		}
	}()

	tracer := tel.tracerProvider.Tracer("mini-cloud.tests.observability-demo")

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		util.Fprintln(w, "ok")
	})
	mux.HandleFunc("/demo", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "demo-request")
		defer span.End()

		probeID := r.URL.Query().Get("probe_id")
		traceID := span.SpanContext().TraceID().String()
		tel.requestCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("endpoint", "/demo")))

		logLine := map[string]any{
			"event":    "demo_request",
			"probe_id": probeID,
			"trace_id": traceID,
			"path":     r.URL.Path,
			"time":     time.Now().UTC().Format(time.RFC3339Nano),
		}
		raw, _ := json.Marshal(logLine)
		fmt.Println(string(raw))

		flushCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := tel.tracerProvider.ForceFlush(flushCtx); err != nil {
			logger.Warn("trace force flush failed", "error", err)
		}
		if err := tel.meterProvider.ForceFlush(flushCtx); err != nil {
			logger.Warn("metric force flush failed", "error", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"probeID": probeID,
			"traceID": traceID,
		})
	})

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("starting observability demo app", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("demo app exited", "error", err)
		os.Exit(1)
	}
}

func newTelemetry(ctx context.Context) (*telemetry, func(context.Context) error, error) {
	res, err := resource.New(ctx, resource.WithFromEnv())
	if err != nil {
		return nil, nil, fmt.Errorf("build resource: %w", err)
	}

	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("create trace exporter: %w", err)
	}
	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("create metric exporter: %w", err)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter, sdktrace.WithBatchTimeout(200*time.Millisecond)),
		sdktrace.WithResource(res),
	)
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(time.Second))),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)

	counter, err := meterProvider.Meter("mini-cloud.tests.observability-demo").Int64Counter(
		"demo_requests",
		metric.WithDescription("How many demo requests the local observability app has handled."),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create counter: %w", err)
	}

	return &telemetry{
			tracerProvider: tracerProvider,
			meterProvider:  meterProvider,
			requestCounter: counter,
		}, func(ctx context.Context) error {
			if err := meterProvider.ForceFlush(ctx); err != nil {
				return err
			}
			if err := tracerProvider.ForceFlush(ctx); err != nil {
				return err
			}
			if err := meterProvider.Shutdown(ctx); err != nil {
				return err
			}
			return tracerProvider.Shutdown(ctx)
		}, nil
}
