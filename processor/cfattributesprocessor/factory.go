package cfattributesprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/cfattributesprocessor"

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/cfattributesprocessor/internal/metadata"
)

var (
	typeStr              = component.MustNewType("cfattributesprocessor")
	consumerCapabilities = consumer.Capabilities{MutatesData: true}
)

const (
	defaultCacheTTL                = 10 * time.Minute
	defaultAppIDAttrKeyAssociation = "app_id"
)

func NewFactory() processor.Factory {
	return processor.NewFactory(
		metadata.Type,
		createDefaultConfig,
		processor.WithMetrics(createMetricsProcessor, metadata.MetricsStability),
		processor.WithLogs(createLogsProcessor, metadata.LogsStability),
		processor.WithTraces(createTracesProcessor, metadata.TracesStability),
	)
}

func createDefaultConfig() component.Config {
	metadata := CfTagExtractMetadata{
		Space: false,
		Org:   false,
		App:   true,
	}
	extract := CfTagExtract{
		Metadata:          metadata,
		AppStateLifecycle: false,
		AppDates:          false,
	}
	return &Config{
		CacheTTL:                     defaultCacheTTL,
		Extract:                      extract,
		AppIDAttributeKeyAssociation: defaultAppIDAttrKeyAssociation,
	}
}

func createMetricsProcessor(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	nextConsumer consumer.Metrics,
) (processor.Metrics, error) {
	processorConfig := cfg.(*Config)
	metricsProcessor := newCFAttributesProcessor(processorConfig, set.Logger)
	return processorhelper.NewMetrics(
		ctx,
		set,
		cfg,
		nextConsumer,
		metricsProcessor.processMetrics,
		processorhelper.WithCapabilities(consumerCapabilities),
		processorhelper.WithStart(metricsProcessor.Start),
		processorhelper.WithShutdown(metricsProcessor.Shutdown),
	)
}

func createLogsProcessor(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	nextConsumer consumer.Logs,
) (processor.Logs, error) {
	processorConfig := cfg.(*Config)
	logsProcessor := newCFAttributesProcessor(processorConfig, set.Logger)
	return processorhelper.NewLogs(
		ctx,
		set,
		cfg,
		nextConsumer,
		logsProcessor.processLogs,
		processorhelper.WithCapabilities(consumerCapabilities),
		processorhelper.WithStart(logsProcessor.Start),
		processorhelper.WithShutdown(logsProcessor.Shutdown),
	)
}

func createTracesProcessor(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	nextConsumer consumer.Traces,
) (processor.Traces, error) {
	processorConfig := cfg.(*Config)
	tracesProcessor := newCFAttributesProcessor(processorConfig, set.Logger)
	return processorhelper.NewTraces(
		ctx,
		set,
		cfg,
		nextConsumer,
		tracesProcessor.processTraces,
		processorhelper.WithCapabilities(consumerCapabilities),
		processorhelper.WithStart(tracesProcessor.Start),
		processorhelper.WithShutdown(tracesProcessor.Shutdown),
	)
}
