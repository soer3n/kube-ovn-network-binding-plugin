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

package server_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	v1 "kubevirt.io/api/core/v1"
	hooksInfo "kubevirt.io/kubevirt/pkg/hooks/info"
	hooksV1alpha3 "kubevirt.io/kubevirt/pkg/hooks/v1alpha3"
	domainschema "kubevirt.io/kubevirt/pkg/virt-launcher/virtwrap/api"

	"github.com/soer3n/kube-ovn-network-binding-plugin/sidecar/domain"
	srv "github.com/soer3n/kube-ovn-network-binding-plugin/sidecar/server"
)

const testMAC = "02:00:00:00:00:01"

func draVMI() []byte {
	vmi := v1.VirtualMachineInstance{
		Spec: v1.VirtualMachineInstanceSpec{
			Networks: []v1.Network{
				{Name: "default", NetworkSource: v1.NetworkSource{Pod: &v1.PodNetwork{}}},
				{Name: "kubeovn0", NetworkSource: v1.NetworkSource{ResourceClaim: &v1.ClaimRequest{ClaimName: "claim", RequestName: "nic0"}}},
			},
			Domain: v1.DomainSpec{Devices: v1.Devices{Interfaces: []v1.Interface{
				{Name: "default", InterfaceBindingMethod: v1.InterfaceBindingMethod{Bridge: &v1.InterfaceBridge{}}},
				{Name: "kubeovn0", Binding: &v1.PluginBinding{Name: domain.BindingName}},
			}}},
		},
	}
	raw, err := json.Marshal(vmi)
	Expect(err).ToNot(HaveOccurred())
	return raw
}

var _ = Describe("InfoServer", func() {
	It("announces the plugin name, version and hook points", func() {
		info, err := srv.InfoServer{Version: "v1alpha3"}.Info(context.Background(), &hooksInfo.InfoParams{})
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Name).To(Equal(srv.PluginName))
		Expect(info.Versions).To(ConsistOf("v1alpha3"))
		var hookPoints []string
		for _, hp := range info.HookPoints {
			hookPoints = append(hookPoints, hp.Name)
		}
		Expect(hookPoints).To(ConsistOf(hooksInfo.OnDefineDomainHookPointName, hooksInfo.ShutdownHookPointName))
	})
})

var _ = Describe("V1alpha3Server", func() {
	resolver := func(string) (string, error) { return testMAC, nil }

	It("adds the plugin-bound interface to the domain", func() {
		s := srv.V1alpha3Server{MACResolver: resolver}
		result, err := s.OnDefineDomain(context.Background(), &hooksV1alpha3.OnDefineDomainParams{
			Vmi:       draVMI(),
			DomainXML: []byte("<domain type='kvm'><name>test</name></domain>"),
		})
		Expect(err).ToNot(HaveOccurred())

		spec := &domainschema.DomainSpec{}
		Expect(xml.Unmarshal(result.GetDomainXML(), spec)).To(Succeed())
		Expect(spec.Name).To(Equal("test"))
		Expect(spec.Devices.Interfaces).To(HaveLen(1))
		Expect(spec.Devices.Interfaces[0].Target.Device).To(Equal(domain.PodInterfaceName("kubeovn0")))
		Expect(spec.Devices.Interfaces[0].MAC.MAC).To(Equal(testMAC))
	})

	It("rejects an invalid VMI", func() {
		_, err := srv.V1alpha3Server{MACResolver: resolver}.OnDefineDomain(context.Background(), &hooksV1alpha3.OnDefineDomainParams{
			Vmi:       []byte("not json"),
			DomainXML: []byte("<domain/>"),
		})
		Expect(err).To(MatchError(ContainSubstring("unmarshal VMI")))
	})

	It("fails when the pod interface cannot be found", func() {
		_, err := srv.V1alpha3Server{}.OnDefineDomain(context.Background(), &hooksV1alpha3.OnDefineDomainParams{
			Vmi:       draVMI(),
			DomainXML: []byte("<domain/>"),
		})
		Expect(err).To(MatchError(ContainSubstring(domain.PodInterfaceName("kubeovn0"))))
	})

	It("passes cloud-init data through", func() {
		result, err := srv.V1alpha3Server{}.PreCloudInitIso(context.Background(), &hooksV1alpha3.PreCloudInitIsoParams{CloudInitData: []byte("data")})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.GetCloudInitData()).To(Equal([]byte("data")))
	})
})

var _ = Describe("Serve", func() {
	It("stops gracefully on a Shutdown call", func() {
		socket, err := net.Listen("unix", filepath.Join(GinkgoT().TempDir(), "hook.sock"))
		Expect(err).ToNot(HaveOccurred())
		done := make(chan struct{})
		grpcServer := grpc.NewServer()
		hooksV1alpha3.RegisterCallbacksServer(grpcServer, srv.V1alpha3Server{Done: done})

		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			srv.Serve(grpcServer, socket, done)
		}()

		conn, err := grpc.NewClient("unix://"+socket.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		Expect(err).ToNot(HaveOccurred())
		defer func() { _ = conn.Close() }()
		_, err = hooksV1alpha3.NewCallbacksClient(conn).Shutdown(context.Background(), &hooksV1alpha3.ShutdownParams{})
		Expect(err).ToNot(HaveOccurred())
		Eventually(stopped, 5*time.Second).Should(BeClosed())
	})

	It("returns when the server fails", func() {
		socket, err := net.Listen("unix", filepath.Join(GinkgoT().TempDir(), "hook.sock"))
		Expect(err).ToNot(HaveOccurred())
		Expect(socket.Close()).To(Succeed())

		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			srv.Serve(grpc.NewServer(), socket, make(chan struct{}))
		}()
		Eventually(stopped, 5*time.Second).Should(BeClosed())
	})
})
