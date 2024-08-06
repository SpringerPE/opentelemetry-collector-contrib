// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cfgardenobserver // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer/cfgardenobserver"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"code.cloudfoundry.org/garden"
	gardenClient "code.cloudfoundry.org/garden/client"
	gardenConnection "code.cloudfoundry.org/garden/client/connection"
	"github.com/cloudfoundry/go-cfclient/v3/client"
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
	once   *sync.Once

	garden garden.Client
	cf     *client.Client

	containerMu sync.RWMutex
	containers  []garden.ContainerInfo
}

var _ extension.Extension = (*cfGardenObserver)(nil)

func newObserver(config *Config, logger *zap.Logger) (extension.Extension, error) {
	g := &cfGardenObserver{
		config: config,
		logger: logger,
		once:   &sync.Once{},
		cancel: func() {
			// Safe value provided on initialisation
		},
	}
	g.EndpointsWatcher = observer.NewEndpointsWatcher(g, config.RefreshInterval, logger)
	return g, nil
}

// TODO: implement caching, cache container list in ListEndpoints, cache app info on a separate goroutine
// TODO: deal with several ports, an option is making an endpoint per port, like the dockerobserver does
func (g *cfGardenObserver) Start(ctx context.Context, _ component.Host) error {
	gCtx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	g.ctx = gCtx

	g.garden = gardenClient.New(gardenConnection.New("unix", g.config.Garden.Endpoint))

	var err error
	g.cf, err = NewCfClient(g.config.CloudFoundry)
	if err != nil {
		return err
	}

	// g.once.Do(
	// 	func() {
	// 		go func() {
	// 			cacheRefreshTicker := time.NewTicker(d.config.CacheSyncInterval)
	// 			defer cacheRefreshTicker.Stop()
	//
	// 			clientCtx, clientCancel := context.WithCancel(d.ctx)
	//
	// 			go d.dClient.ContainerEventLoop(clientCtx)
	//
	// 			for {
	// 				select {
	// 				case <-d.ctx.Done():
	// 					clientCancel()
	// 					return
	// 				case <-cacheRefreshTicker.C:
	// 					err = d.dClient.LoadContainerList(clientCtx)
	// 					if err != nil {
	// 						d.logger.Error("Could not sync container cache", zap.Error(err))
	// 					}
	// 				}
	// 			}
	// 		}()
	// 	},
	// )

	return nil
}

func (g *cfGardenObserver) Shutdown(ctx context.Context) error {
	g.cancel()
	return nil
}

func (g *cfGardenObserver) ListEndpoints() []observer.Endpoint {
	g.logger.Info("STARTING LIST ENDPOINTS")
	var endpoints []observer.Endpoint

	containers, err := g.garden.Containers(garden.Properties{})
	if err != nil {
		g.logger.Error("could not list containers", zap.Error(err))
		return endpoints
	}
	g.logger.Info(fmt.Sprintf("Count from calling garden.Containers: %d", len(containers)))
	g.logger.Info(fmt.Sprintf("Cache size: %d", len(g.containers)))

	infos := []garden.ContainerInfo{}
	for _, c := range containers {
		info, err := c.Info()
		if err != nil {
			g.logger.Error("could not get info for container", zap.String("handle", c.Handle()), zap.Error(err))
			continue
		}

		if info.State != "active" {
			continue
		}

		endpoint, err := g.containerEndpoint(c.Handle(), info)
		if err != nil {
			g.logger.Error("error creating container endpoint", zap.Error(err))
			continue
		}
		endpoints = append(endpoints, *endpoint)
		infos = append(infos, info)
	}

	go g.updateContainerCache(infos)
	g.logger.Info(fmt.Sprintf("Count of endpoints: %d", len(endpoints)))
	g.logger.Info("FINISHED LIST ENDPOINTS")
	return endpoints
}

func (g *cfGardenObserver) updateContainerCache(infos []garden.ContainerInfo) {
	g.containerMu.Lock()
	defer g.containerMu.Unlock()
	g.containers = infos
}

func (g *cfGardenObserver) containerEndpoint(handle string, info garden.ContainerInfo) (*observer.Endpoint, error) {
	rawPort, ok := info.Properties["network.ports"]
	if !ok {
		return nil, errors.New("could not discover port for container")
	}

	port, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("container port is not valid: %v", err)
	}

	appId, ok := info.Properties["network.app_id"]
	if !ok {
		g.logger.Warn("container is not part of an application")
	}

	app, err := g.cf.Applications.Get(g.ctx, appId)
	if err != nil {
		g.logger.Warn("error fetching application", zap.Error(err))
		return nil, fmt.Errorf("error fetching application: %v", err)
	}

	details := &observer.Container{
		Name:        handle,
		ContainerID: handle,
		Host:        info.ContainerIP,
		Port:        uint16(port),
		Transport:   observer.ProtocolTCP,
		Labels:      g.containerLabels(info, app),
	}

	endpoint := &observer.Endpoint{
		ID:      observer.EndpointID(details.ContainerID),
		Target:  fmt.Sprintf("%s:%d", details.Host, details.Port),
		Details: details,
	}

	return endpoint, nil
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
