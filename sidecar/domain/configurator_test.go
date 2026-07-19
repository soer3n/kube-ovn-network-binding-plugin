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

package domain_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "kubevirt.io/api/core/v1"
	domainschema "kubevirt.io/kubevirt/pkg/virt-launcher/virtwrap/api"

	"github.com/soer3n/kube-ovn-network-binding-plugin/sidecar/domain"
)

func draNetwork(name, claimName, requestName string) v1.Network {
	return v1.Network{
		Name: name,
		NetworkSource: v1.NetworkSource{
			ResourceClaim: &v1.ClaimRequest{
				ClaimName:   claimName,
				RequestName: requestName,
			},
		},
	}
}

func boundInterface(name string) v1.Interface {
	return v1.Interface{
		Name:    name,
		Binding: &v1.PluginBinding{Name: domain.BindingName},
	}
}

// fakeMACResolver returns mac for any podIfaceName, standing in for a real
// netlink lookup (there is no real tap/veth present in these unit tests).
func fakeMACResolver(mac string) domain.MACResolver {
	return func(string) (string, error) { return mac, nil }
}

var _ = Describe("Configurator", func() {
	It("appends an ethernet interface targeting the hashed pod interface name, with the resolved (not VMI-spec) MAC", func() {
		iface := boundInterface("nic1")
		net := draNetwork("nic1", "my-claim", "nic1-request")

		configurator := domain.NewConfiguratorWithMACResolver([]v1.Interface{iface}, []v1.Network{net}, fakeMACResolver("02:00:00:00:00:01"))
		domainSpec, err := configurator.Mutate(&domainschema.DomainSpec{})
		Expect(err).ToNot(HaveOccurred())
		Expect(domainSpec.Devices.Interfaces).To(HaveLen(1))

		got := domainSpec.Devices.Interfaces[0]
		Expect(got.Type).To(Equal("ethernet"))
		Expect(got.Target).ToNot(BeNil())
		Expect(got.Target.Device).To(Equal(domain.PodInterfaceName("nic1")))
		Expect(got.MAC).ToNot(BeNil())
		Expect(got.MAC.MAC).To(Equal("02:00:00:00:00:01"))
		Expect(got.Model).ToNot(BeNil())
		Expect(got.Model.Type).To(Equal(v1.VirtIO))
	})

	It("ignores interfaces not bound to this plugin", func() {
		iface := v1.Interface{Name: "default", InterfaceBindingMethod: v1.InterfaceBindingMethod{Masquerade: &v1.InterfaceMasquerade{}}}
		net := v1.Network{Name: "default", NetworkSource: v1.NetworkSource{Pod: &v1.PodNetwork{}}}

		configurator := domain.NewConfigurator([]v1.Interface{iface}, []v1.Network{net})
		domainSpec, err := configurator.Mutate(&domainschema.DomainSpec{})
		Expect(err).ToNot(HaveOccurred())
		Expect(domainSpec.Devices.Interfaces).To(BeEmpty())
	})

	It("errors when no network matches the bound interface", func() {
		iface := boundInterface("nic1")

		configurator := domain.NewConfigurator([]v1.Interface{iface}, nil)
		_, err := configurator.Mutate(&domainschema.DomainSpec{})
		Expect(err).To(HaveOccurred())
	})

	It("errors when the matched network is not a DRA network", func() {
		iface := boundInterface("nic1")
		net := v1.Network{Name: "nic1", NetworkSource: v1.NetworkSource{Multus: &v1.MultusNetwork{NetworkName: "some-nad"}}}

		configurator := domain.NewConfigurator([]v1.Interface{iface}, []v1.Network{net})
		_, err := configurator.Mutate(&domainschema.DomainSpec{})
		Expect(err).To(HaveOccurred())
	})

	// kube-ovn's OVN logical switch port enforces port security keyed on the
	// exact MAC it allocated via IPAM. That MAC is never in the VMI spec (a
	// DRA network's MAC is decided by kube-ovn, not the VMI author), so
	// Mutate must fail loudly if it can't resolve the real one rather than
	// silently emitting an interface with no (or the wrong) MAC — leaving it
	// unset let libvirt invent its own fallback MAC on a live cluster, which
	// didn't match kube-ovn's and got the guest's traffic dropped at the
	// switch.
	It("errors when the MAC cannot be resolved", func() {
		iface := boundInterface("nic1")
		net := draNetwork("nic1", "my-claim", "nic1-request")

		configurator := domain.NewConfiguratorWithMACResolver([]v1.Interface{iface}, []v1.Network{net},
			func(string) (string, error) { return "", fmt.Errorf("link not found") })
		_, err := configurator.Mutate(&domainschema.DomainSpec{})
		Expect(err).To(HaveOccurred())
	})

	// Regression: virt-launcher can call OnDefineDomain more than once
	// against a domain spec that already carries our interface from an
	// earlier call in the same start attempt. An unguarded second append hit
	// libvirt error "unsupported configuration: non unique alias detected:
	// ua-kubeovn0" on a live cluster.
	It("does not duplicate an interface already present in the domain spec (repeated OnDefineDomain call)", func() {
		iface := boundInterface("nic1")
		net := draNetwork("nic1", "my-claim", "nic1-request")
		configurator := domain.NewConfiguratorWithMACResolver([]v1.Interface{iface}, []v1.Network{net}, fakeMACResolver("02:00:00:00:00:01"))

		domainSpec, err := configurator.Mutate(&domainschema.DomainSpec{})
		Expect(err).ToNot(HaveOccurred())
		Expect(domainSpec.Devices.Interfaces).To(HaveLen(1))

		domainSpec, err = configurator.Mutate(domainSpec)
		Expect(err).ToNot(HaveOccurred())
		Expect(domainSpec.Devices.Interfaces).To(HaveLen(1))
	})
})
