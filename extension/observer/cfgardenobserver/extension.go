// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cfgardenobserver // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer/cfgardenobserver"

import (
	"context"
	"fmt"
	"strings"
	"time"

	"code.cloudfoundry.org/garden"
	gardenClient "code.cloudfoundry.org/garden/client"
	gardenConnection "code.cloudfoundry.org/garden/client/connection"
	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
	"go.uber.org/zap"
)

type cfGardenObserver struct {
	*observer.EndpointsWatcher
	cancel func()
	client garden.Client
	config *Config
	ctx    context.Context
	logger *zap.Logger
}

var _ extension.Extension = (*cfGardenObserver)(nil)

func newObserver(settings extension.Settings, config *Config, logger *zap.Logger) (extension.Extension, error) {
	g := &cfGardenObserver{
		config: config,
		logger: logger,
		cancel: func() {
		},
	}
	g.EndpointsWatcher = observer.NewEndpointsWatcher(g, time.Second, settings.Logger)

	return g, nil
}

func (g *cfGardenObserver) Start(ctx context.Context, _ component.Host) error {
	gCtx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	g.ctx = gCtx

	g.client = gardenClient.New(gardenConnection.New("unix", g.config.Endpoint))
	return nil
}

func (g *cfGardenObserver) Shutdown(_ context.Context) error {
	g.cancel()
	return nil
}

func (g *cfGardenObserver) ListEndpoints() []observer.Endpoint {
	var endpoints []observer.Endpoint

	containers, err := g.client.Containers(garden.Properties{})
	if err != nil {
		g.logger.Error("could not list containers", zap.Error(err))
		return endpoints
	}

	for _, c := range containers {
		endpoint := g.containerEndpoint(c)
		if endpoint != nil {
			endpoints = append(endpoints, *endpoint)
		}
	}

	return endpoints
}

func (g *cfGardenObserver) containerEndpoint(c garden.Container) *observer.Endpoint {
	info, err := c.Info()
	if err != nil {
		g.logger.Error("could not get info for container", zap.Error(err))
		return nil
	}

	if info.State != "active" {
		return nil
	}

	handle := c.Handle()
	details := &observer.Container{
		Name:          handle, //TODO use application name / container id
		ContainerID:   handle,
		Host:          info.ContainerIP,
		Port:          8080,
		AlternatePort: 61001,
		Transport:     observer.ProtocolTCP,
		Labels:        containerLabels(info),
	}

	endpoint := observer.Endpoint{
		ID:      observer.EndpointID(details.ContainerID),
		Target:  fmt.Sprintf("%s:%d", details.Host, details.Port),
		Details: details,
	}

	return &endpoint
}

func containerLabels(info garden.ContainerInfo) map[string]string {
	labels := make(map[string]string)
	for k, v := range info.Properties {
		if strings.HasPrefix(k, "network") {
			labels[k] = v
		}
	}
	return labels
}
