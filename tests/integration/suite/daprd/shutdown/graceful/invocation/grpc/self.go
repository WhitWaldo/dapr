/*
Copyright 2025 The Dapr Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package grpc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1 "github.com/dapr/dapr/pkg/proto/common/v1"
	rtv1 "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"github.com/dapr/dapr/tests/integration/framework"
	"github.com/dapr/dapr/tests/integration/framework/os"
	"github.com/dapr/dapr/tests/integration/framework/process/daprd"
	"github.com/dapr/dapr/tests/integration/framework/process/grpc/app"
	"github.com/dapr/dapr/tests/integration/suite"
)

func init() {
	suite.Register(new(self))
}

type self struct {
	daprd *daprd.Daprd

	inInvoke    atomic.Bool
	closeInvoke chan struct{}
}

func (s *self) Setup(t *testing.T) []framework.Option {
	os.SkipWindows(t)

	s.closeInvoke = make(chan struct{})

	app := app.New(t,
		app.WithOnInvokeFn(func(context.Context, *commonv1.InvokeRequest) (*commonv1.InvokeResponse, error) {
			s.inInvoke.Store(true)
			<-s.closeInvoke
			return nil, nil
		}),
	)

	s.daprd = daprd.New(t,
		daprd.WithAppPort(app.Port(t)),
		daprd.WithAppProtocol("grpc"),
		daprd.WithDaprGracefulShutdownSeconds(180),
		daprd.WithAppHealthProbeInterval(1),
		daprd.WithAppHealthProbeThreshold(1),
		daprd.WithAppHealthCheck(true),
	)

	return []framework.Option{
		framework.WithProcesses(app),
	}
}

func (s *self) Run(t *testing.T, ctx context.Context) {
	s.daprd.Run(t, ctx)
	t.Cleanup(func() { s.daprd.Cleanup(t) })

	client := s.daprd.GRPCClient(t, ctx)

	errCh := make(chan error)
	go func() {
		_, err := client.InvokeService(ctx, &rtv1.InvokeServiceRequest{
			Id: s.daprd.AppID(),
			Message: &commonv1.InvokeRequest{
				Method:        "foo",
				HttpExtension: &commonv1.HTTPExtension{Verb: commonv1.HTTPExtension_POST},
			},
		})

		errCh <- err
	}()

	require.Eventually(t, s.inInvoke.Load, time.Second*10, time.Millisecond*10)

	go s.daprd.Cleanup(t)

	select {
	case err := <-errCh:
		assert.Fail(t, "unexpected error returned", err)
	case <-time.After(time.Second * 3):
	}

	close(s.closeInvoke)

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(time.Second * 10):
		assert.Fail(t, "timeout")
	}
}
