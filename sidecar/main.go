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

package main

import (
	"net"
	"os"
	"path/filepath"

	"google.golang.org/grpc"

	"kubevirt.io/client-go/log"
	"kubevirt.io/kubevirt/pkg/hooks"
	hooksInfo "kubevirt.io/kubevirt/pkg/hooks/info"
	hooksV1alpha3 "kubevirt.io/kubevirt/pkg/hooks/v1alpha3"

	srv "github.com/soer3n/kube-ovn-network-binding-plugin/sidecar/server"
)

const hookSocket = "kube-ovn-nic.sock"
const sidecarAPIVersion = "v1alpha3"
const sidecarName = "kube-ovn-network-binding-sidecar"

func main() {
	log.InitializeLogging(sidecarName)
	socketPath := filepath.Join(hooks.HookSocketsSharedDirectory, hookSocket)
	socket, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Log.Reason(err).Errorf("failed to initialize socket on path: %s", socketPath)
		os.Exit(1)
	}
	defer func() {
		if err := os.Remove(socketPath); err != nil {
			log.Log.Reason(err).Errorf("failed to remove socket: %s", socketPath)
		}
	}()

	grpcServer := grpc.NewServer()
	hooksInfo.RegisterInfoServer(grpcServer, srv.InfoServer{Version: sidecarAPIVersion})

	shutdownChan := make(chan struct{})
	hooksV1alpha3.RegisterCallbacksServer(grpcServer, srv.V1alpha3Server{Done: shutdownChan})

	log.Log.Infof("starting kube-ovn network binding hook server on socket %q using API version %s", socketPath, sidecarAPIVersion)
	srv.Serve(grpcServer, socket, shutdownChan)
}
