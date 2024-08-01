// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cfgardenobserver

import (
	"testing"

	"code.cloudfoundry.org/garden"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// func TestStartAndStopObserver(t *testing.T) {
// 	factory := NewFactory()
// 	ext, err := newObserver(factory.CreateDefaultConfig().(*Config), zap.NewNop())
// 	require.NoError(t, err)
// 	require.NotNil(t, ext)
//
// 	obs, ok := ext.(*cfGardenObserver)
// 	require.True(t, ok)
//
// 	ctx := context.Background()
// 	require.NoError(t, obs.Start(ctx, componenttest.NewNopHost()))
//
// 	expected := obs.ListEndpoints()
// 	want := []observer.Endpoint{}
// 	require.Equal(t, want, expected)
//
// 	require.NoError(t, obs.Shutdown(ctx))
// }

func TestContainerLabels(t *testing.T) {
	input := garden.ContainerInfo{
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
	}

	factory := NewFactory()
	ext, err := newObserver(factory.CreateDefaultConfig().(*Config), zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, ext)

	obs, ok := ext.(*cfGardenObserver)
	require.True(t, ok)

	require.Equal(t, expected, obs.containerLabels(input))
}
