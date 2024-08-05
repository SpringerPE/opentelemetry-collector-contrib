// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cfgardenobserver // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer/cfgardenobserver"

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"code.cloudfoundry.org/garden"
	gardenClient "code.cloudfoundry.org/garden/client"
	gardenConnection "code.cloudfoundry.org/garden/client/connection"
	"github.com/cloudfoundry/go-cfclient/v3/client"
	"github.com/cloudfoundry/go-cfclient/v3/config"
	"github.com/cloudfoundry/go-cfclient/v3/resource"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer"
)

type cfGardenObserver struct {
	*observer.EndpointsWatcher
	cancel context.CancelFunc
	config *Config
	ctx    context.Context
	logger *zap.Logger

	garden garden.Client
	cf     *client.Client
}

var _ extension.Extension = (*cfGardenObserver)(nil)

func newObserver(config *Config, logger *zap.Logger) (extension.Extension, error) {
	g := &cfGardenObserver{
		config: config,
		logger: logger,
		cancel: func() {
		},
	}
	g.EndpointsWatcher = observer.NewEndpointsWatcher(g, time.Second, logger)

	return g, nil
}

// TODO: make credentials part of the configuration
// need to find a way to set it up so automatic tests can still run (option of no credentials?)
func (g *cfGardenObserver) Start(ctx context.Context, _ component.Host) error {
	gCtx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	g.ctx = gCtx

	g.garden = gardenClient.New(gardenConnection.New("unix", g.config.Endpoint))

	cfg, err := config.New("https://api.t.snpaas.eu", config.UserPassword("admin", "pass"))
	if err != nil {
		return err
	}
	g.cf, err = client.New(cfg)
	if err != nil {
		return err
	}

	return nil
}

func (g *cfGardenObserver) Shutdown(_ context.Context) error {
	g.cancel()
	return nil
}

func (g *cfGardenObserver) ListEndpoints() []observer.Endpoint {
	var endpoints []observer.Endpoint

	containers, err := g.garden.Containers(garden.Properties{})
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

	rawPort, ok := info.Properties["network.ports"]
	if !ok {
		g.logger.Warn("could not discover port for container for port")
		return nil
	}

	port, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil {
		g.logger.Warn("container port is not valid", zap.Error(err))
		return nil
	}

	appId, ok := info.Properties["network.app_id"]
	if !ok {
		g.logger.Warn("container is not part of an application")
	}
	app, err := g.cf.Applications.Get(g.ctx, appId)
	if err != nil {
		g.logger.Warn("error fetching application", zap.Error(err))
		return nil
	}

	details := &observer.Container{
		Name:        c.Handle(),
		ContainerID: c.Handle(),
		Host:        info.ContainerIP,
		Port:        uint16(port),
		Transport:   observer.ProtocolTCP,
		Labels:      g.containerLabels(info, app),
	}

	endpoint := observer.Endpoint{
		ID:      observer.EndpointID(details.ContainerID),
		Target:  fmt.Sprintf("%s:%d", details.Host, details.Port),
		Details: details,
	}

	return &endpoint
}

func (g *cfGardenObserver) containerLabels(info garden.ContainerInfo, app *resource.App) map[string]string {
	labels := make(map[string]string)
	tags, err := parseTags(info)
	if err != nil {
		g.logger.Warn("not able to parse container tags into labels", zap.Error(err))
		return nil
	}
	for k, v := range tags {
		labels[k] = v
	}

	for k, v := range app.Metadata.Labels {
		labels[k] = *v
	}

	return labels
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
