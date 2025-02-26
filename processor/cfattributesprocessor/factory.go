package cfattributesprocessor // import "cfattributesprocessor"

import (
	"context"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/cfattributesprocessor/internal/metadata"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
)

var typeStr = component.MustNewType("cfattributesprocessor")

const (
	defaultCollectionInterval = 1 * time.Minute
	defaultCacheSyncInterval  = 5 * time.Minute
)

func NewFactory() processor.Factory {
	return processor.NewFactory(
		metadata.Type,
		createDefaultConfig,
		processor.WithMetrics(createMetricsProcessor, metadata.MetricsStability),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		RefreshInterval:   defaultCollectionInterval,
		CacheSyncInterval: defaultCacheSyncInterval,
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
		metricsProcessor.processMetrics)
}
