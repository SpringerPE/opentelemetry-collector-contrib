// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cfgardenobserver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/extension/extensiontest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer"
)

func TestStartAndStopObserver(t *testing.T) {
	factory := NewFactory()
	params := extensiontest.NewNopSettings()
	ext, err := newObserver(params, factory.CreateDefaultConfig().(*Config))
	require.NoError(t, err)
	require.NotNil(t, ext)

	obvs, ok := ext.(*cfGardenObserver)
	require.True(t, ok)

	ctx := context.Background()
	require.NoError(t, obvs.Start(ctx, componenttest.NewNopHost()))

	expected := obvs.ListEndpoints()
	want := []observer.Endpoint{}
	require.Equal(t, want, expected)

	time.Sleep(500 * time.Millisecond) // Wait a bit to sync endpoints once.
	require.NoError(t, obvs.Shutdown(ctx))
}
