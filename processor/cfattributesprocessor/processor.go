package cfattributesprocessor // import "cfattributesprocessor"

import (
	"context"
	"fmt"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

type cfAttributesProcessor struct {
	// ConfiguredMetrics map[string]bool
	logger *zap.Logger
}

func newCFAttributesProcessor(config *Config, logger *zap.Logger) *cfAttributesProcessor {
	return &cfAttributesProcessor{
		// ConfiguredMetrics: inputMetricSet,
		logger: logger,
	}
}

func (cfap *cfAttributesProcessor) Start(context.Context, component.Host) error {
	return nil
}

func (cfap *cfAttributesProcessor) processMetrics(_ context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	resourceMetricsSlice := md.ResourceMetrics()

	for i := 0; i < resourceMetricsSlice.Len(); i++ {
		rm := resourceMetricsSlice.At(i)
		ilms := rm.ScopeMetrics()
		for i := 0; i < ilms.Len(); i++ {
			ilm := ilms.At(i)
			metricSlice := ilm.Metrics()
			for j := 0; j < metricSlice.Len(); j++ {
				metric := metricSlice.At(j)
				fmt.Printf(" metric name : %v", metric.Name())
				// 		if _, ok := cfap.ConfiguredMetrics[metric.Name()]; !ok {
				// 			continue
				// 		}
				// 		if metric.Type() != pmetric.MetricTypeSum || metric.Sum().AggregationTemporality() != pmetric.AggregationTemporalityDelta {
				// 			cfap.logger.Info(fmt.Sprintf("Configured metric for rate calculation %s is not a delta sum\n", metric.Name()))
				// 			continue
				// 		}
				// 		dataPointSlice := metric.Sum().DataPoints()

				// 		for i := 0; i < dataPointSlice.Len(); i++ {
				// 			dataPoint := dataPointSlice.At(i)

				// 			durationNanos := time.Duration(dataPoint.Timestamp() - dataPoint.StartTimestamp())
				// 			var rate float64
				// 			switch dataPoint.ValueType() {
				// 			case pmetric.NumberDataPointValueTypeDouble:
				// 				rate = calculateRate(dataPoint.DoubleValue(), durationNanos)
				// 			case pmetric.NumberDataPointValueTypeInt:
				// 				rate = calculateRate(float64(dataPoint.IntValue()), durationNanos)
				// 			default:
				// 				return md, consumererror.NewPermanent(fmt.Errorf("invalid data point type:%d", dataPoint.ValueType()))
				// 			}
				// 			dataPoint.SetDoubleValue(rate)
				// 		}

				// 		// Setting the data type removed all the data points, so we must move them back to the metric.
				// 		dataPointSlice.MoveAndAppendTo(metric.SetEmptyGauge().DataPoints())
			}
		}
	}
	return md, nil
}

func (cfap *cfAttributesProcessor) Shutdown(context.Context) error {
	return nil
}
