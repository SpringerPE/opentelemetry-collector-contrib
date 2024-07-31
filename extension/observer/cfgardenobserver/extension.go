// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cfgardenobserver // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer/cfgardenobserver"

import (
	"context"
	"encoding/json"
	"fmt"
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
		Name:          handle, // TODO use application name / container id
		ContainerID:   handle,
		Host:          info.ContainerIP,
		Port:          8080,
		AlternatePort: 61001,
		Transport:     observer.ProtocolTCP,
		Labels:        g.containerLabels(info),
	}

	endpoint := observer.Endpoint{
		ID:      observer.EndpointID(details.ContainerID),
		Target:  fmt.Sprintf("%s:%d", details.Host, details.Port),
		Details: details,
	}

	return &endpoint
}

func (g *cfGardenObserver) containerLabels(info garden.ContainerInfo) map[string]string {
	tags, err := parseTags(info)
	if err != nil {
		g.logger.Warn("not able to parse container tags into labels", zap.Error(err))
		return nil
	}

	return tags
}

// The info looks like this:
//
//		{
//		  "log_config": {
//		    "guid": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
//		    "index": 0,
//		    "source_name": "CELL",
//		    "tags": {
//		      "app_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
//		      "app_name": "example-app",
//		      "instance_id": "0",
//		      "organization_id": "11111111-2222-3333-4444-555555555555",
//		      "organization_name": "example-org",
//		      "process_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
//		      "process_instance_id": "abcdef12-3456-7890-abcd-ef1234567890",
//		      "process_type": "web",
//		      "source_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
//		      "space_id": "99999999-8888-7777-6666-555555555555",
//		      "space_name": "example-space"
//		    }
//		  }
//		}
//
//	 We parse only the tags into a map, to be used as labels
func parseTags(info garden.ContainerInfo) (map[string]string, error) {
	logConfig, ok := info.Properties["log_config"]
	if !ok {
		return nil, fmt.Errorf("container properties do not have log_config field")
	}

	var data map[string]any
	err := json.Unmarshal([]byte(logConfig), &data)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling logConfig: %v", err)
	}

	tags, ok := data["tags"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected tags field to be a map. got=%T", data["tags"])
	}

	result := make(map[string]string)
	for key, value := range tags {
		if strValue, ok := value.(string); ok {
			result[key] = strValue
		}
	}

	return result, nil
}
