package cfgardenobserver

import (
	"sync"

	"code.cloudfoundry.org/garden"
	gardenClient "code.cloudfoundry.org/garden/client"
	gardenConnection "code.cloudfoundry.org/garden/client/connection"
)

type GardenClient struct {
	garden.Client

	ContainerCache   map[string]garden.ContainerInfo
	ContainerCacheMu sync.RWMutex
}

func NewGardenClient(endpoint string) *GardenClient {
	return &GardenClient{
		Client:         gardenClient.New(gardenConnection.New("unix", endpoint)),
		ContainerCache: make(map[string]garden.ContainerInfo),
	}
}

func (gc *GardenClient) Containers() map[string]garden.ContainerInfo {
	return gc.ContainerCache
}

func (gc *GardenClient) SyncContainerCache() error {
	containers, err := gc.Client.Containers(garden.Properties{})
	if err != nil {
		return err
	}

	gc.ContainerCacheMu.Lock()
	defer gc.ContainerCacheMu.Unlock()

	gc.ContainerCache = make(map[string]garden.ContainerInfo)
	for _, c := range containers {
		info, err := c.Info()
		if err != nil {
			return err
		}
		gc.ContainerCache[c.Handle()] = info
	}
	return nil
}
