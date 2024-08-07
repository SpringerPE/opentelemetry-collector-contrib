// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cfgardenobserver

import (
	"fmt"
	"testing"

	"code.cloudfoundry.org/garden"
	"github.com/cloudfoundry/go-cfclient/v3/resource"
	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func strPtr(s string) *string { return &s }

func TestContainerEndpointsOnePort(t *testing.T) {
	handle := "14d91d46-6ebd-43a1-8e20-316d8e6a92a4"
	ip := "1.2.3.4"
	port := 8080
	appId := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	logConfig := fmt.Sprintf(`
{
    "guid": "%s",
    "index": 0,
    "source_name": "CELL",
    "tags": {
        "app_id": "%s",
        "app_name": "myapp"
    }
}
            `, handle, appId)

	input := garden.ContainerInfo{
		ContainerIP: ip,
		Properties: map[string]string{
			"log_config":     logConfig,
			"network.ports":  "8080",
			"network.app_id": appId,
		},
	}

	expected := []observer.Endpoint{
		{
			ID:     observer.EndpointID(fmt.Sprintf("%s:%d", handle, 8080)),
			Target: fmt.Sprintf("%s:%d", ip, 8080),
			Details: &observer.Container{
				Name:        handle,
				ContainerID: handle,
				Host:        ip,
				Port:        uint16(port),
				Transport:   observer.ProtocolTCP,
				Labels: map[string]string{
					"app_id":     "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
					"app_name":   "myapp",
					"app_label":  "app_value",
					"app_label2": "app_value2",
				},
			},
		},
	}

	factory := NewFactory()
	ext, err := newObserver(factory.CreateDefaultConfig().(*Config), zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, ext)

	obs, ok := ext.(*cfGardenObserver)
	require.True(t, ok)
	obs.apps[appId] = &resource.App{
		Metadata: &resource.Metadata{
			Labels: map[string]*string{
				"app_label":  strPtr("app_value"),
				"app_label2": strPtr("app_value2"),
			},
		},
	}

	require.Equal(t, expected, obs.containerEndpoints(handle, input))
}

func TestContainerLabels(t *testing.T) {
	info := garden.ContainerInfo{
		Properties: map[string]string{
			"log_config": `
{
    "guid": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
    "index": 0,
    "source_name": "CELL",
    "tags": {
        "app_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
        "app_name": "example-app",
        "instance_id": "0",
        "organization_id": "11111111-2222-3333-4444-555555555555",
        "organization_name": "example-org",
        "process_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
        "process_instance_id": "abcdef12-3456-7890-abcd-ef1234567890",
        "process_type": "web",
        "source_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
        "space_id": "99999999-8888-7777-6666-555555555555",
        "space_name": "example-space"
    }
}
            `,
		},
	}
	app := &resource.App{
		Metadata: &resource.Metadata{
			Labels: map[string]*string{
				"key":  strPtr("value"),
				"key2": strPtr("value2"),
			},
		},
	}
	expected := map[string]string{
		"app_id":              "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		"app_name":            "example-app",
		"instance_id":         "0",
		"organization_id":     "11111111-2222-3333-4444-555555555555",
		"organization_name":   "example-org",
		"process_id":          "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		"process_instance_id": "abcdef12-3456-7890-abcd-ef1234567890",
		"process_type":        "web",
		"source_id":           "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		"space_id":            "99999999-8888-7777-6666-555555555555",
		"space_name":          "example-space",
		"key":                 "value",
		"key2":                "value2",
	}

	factory := NewFactory()
	ext, err := newObserver(factory.CreateDefaultConfig().(*Config), zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, ext)

	obs, ok := ext.(*cfGardenObserver)
	require.True(t, ok)

	require.Equal(t, expected, obs.containerLabels(info, app))
}
