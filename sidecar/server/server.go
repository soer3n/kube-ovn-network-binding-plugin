/*
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/log"
	hooksInfo "kubevirt.io/kubevirt/pkg/hooks/info"
	hooksV1alpha3 "kubevirt.io/kubevirt/pkg/hooks/v1alpha3"

	"github.com/soer3n/kube-ovn-network-binding-plugin/sidecar/callback"
	"github.com/soer3n/kube-ovn-network-binding-plugin/sidecar/domain"
)

const PluginName = "kube-ovn-nic"

type InfoServer struct {
	Version string
}

func (s InfoServer) Info(_ context.Context, _ *hooksInfo.InfoParams) (*hooksInfo.InfoResult, error) {
	return &hooksInfo.InfoResult{
		Name: PluginName,
		Versions: []string{
			s.Version,
		},
		HookPoints: []*hooksInfo.HookPoint{
			{
				Name:     hooksInfo.OnDefineDomainHookPointName,
				Priority: 0,
			},
			{
				Name:     hooksInfo.ShutdownHookPointName,
				Priority: 0,
			},
		},
	}, nil
}

type V1alpha3Server struct {
	Done chan struct{}
}

func (s V1alpha3Server) OnDefineDomain(_ context.Context, params *hooksV1alpha3.OnDefineDomainParams) (*hooksV1alpha3.OnDefineDomainResult, error) {
	vmi := &v1.VirtualMachineInstance{}
	if err := json.Unmarshal(params.GetVmi(), vmi); err != nil {
		return nil, fmt.Errorf("failed to unmarshal VMI: %w", err)
	}

	configurator := domain.NewConfigurator(vmi.Spec.Domain.Devices.Interfaces, vmi.Spec.Networks)

	newDomainXML, err := callback.OnDefineDomain(params.GetDomainXML(), configurator)
	if err != nil {
		return nil, err
	}

	return &hooksV1alpha3.OnDefineDomainResult{
		DomainXML: newDomainXML,
	}, nil
}

func (s V1alpha3Server) PreCloudInitIso(_ context.Context, params *hooksV1alpha3.PreCloudInitIsoParams) (*hooksV1alpha3.PreCloudInitIsoResult, error) {
	return &hooksV1alpha3.PreCloudInitIsoResult{
		CloudInitData: params.GetCloudInitData(),
	}, nil
}

func (s V1alpha3Server) Shutdown(_ context.Context, _ *hooksV1alpha3.ShutdownParams) (*hooksV1alpha3.ShutdownResult, error) {
	log.Log.Info("shutdown kube-ovn network binding")
	s.Done <- struct{}{}
	return &hooksV1alpha3.ShutdownResult{}, nil
}

func waitForShutdown(grpcServer *grpc.Server, errChan <-chan error, shutdownChan <-chan struct{}) {
	signalStopChan := make(chan os.Signal, 1)
	signal.Notify(signalStopChan, os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
	var err error
	select {
	case sig := <-signalStopChan:
		log.Log.Infof("received signal: %s", sig.String())
	case err = <-errChan:
		log.Log.Reason(err).Error("grpc server failed")
	case <-shutdownChan:
		log.Log.Info("received shutdown, exiting")
	}

	if err == nil {
		grpcServer.GracefulStop()
	}
}

// Serve runs the given grpc server on socket until a shutdown signal, an
// OS signal, or a Shutdown RPC call is received.
func Serve(grpcServer *grpc.Server, socket net.Listener, shutdownChan <-chan struct{}) {
	errChan := make(chan error)
	go func() {
		errChan <- grpcServer.Serve(socket)
	}()

	waitForShutdown(grpcServer, errChan, shutdownChan)
}
