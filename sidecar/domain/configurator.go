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

package domain

import (
	"crypto/sha256"
	"fmt"

	"github.com/vishvananda/netlink"
	v1 "kubevirt.io/api/core/v1"
	domainschema "kubevirt.io/kubevirt/pkg/virt-launcher/virtwrap/api"
)

// BindingName is the value expected in an Interface's Binding.Name for this
// plugin to act on it. It must match the key used to register this plugin's
// sidecarImage under KubeVirtConfiguration.spec.configuration.network.binding.
const BindingName = "kube-ovn-nic"

// podInterfaceNamePrefix + podInterfaceNameHashLen must stay well under 12
// characters: kube-ovn-dra-driver's vethNames() (pkg/plumbing/plumbing.go)
// computes cid[:12-len(ifaceName)] with no bounds check, so an ifaceName
// longer than 12 chars panics with a negative slice index. KubeVirt's own
// namescheme.GenerateHashedInterfaceName produces 14-char names ("pod" + 11
// hex chars) — safe for KubeVirt's own IFNAMSIZ budget, but too long for
// this driver's tighter one. Do not reuse it here.
const (
	podInterfaceNamePrefix     = "nic"
	podInterfaceNameHashLength = 8
)

// Configurator appends a libvirt "generic ethernet" interface for every VMI
// interface bound to this plugin, targeting the pod-netns device that
// kube-ovn-dra-driver already plumbed. It does not create or discover any
// device itself: the device name is fully determined ahead of time by the
// naming contract (see PodInterfaceName), independent of KubeVirt's own
// (Multus-only) namescheme package.
type Configurator struct {
	interfaces []v1.Interface
	networks   []v1.Network
	resolveMAC MACResolver
}

// MACResolver resolves the actual MAC address of the pod-netns interface
// named podIfaceName. Overridable for tests via NewConfiguratorWithMACResolver;
// NewConfigurator defaults to a real netlink lookup.
type MACResolver func(podIfaceName string) (string, error)

func NewConfigurator(interfaces []v1.Interface, networks []v1.Network) *Configurator {
	return NewConfiguratorWithMACResolver(interfaces, networks, netlinkMACResolver)
}

// NewConfiguratorWithMACResolver is like NewConfigurator but lets callers
// substitute the MAC lookup instead of querying real netlink devices —
// primarily so unit tests don't need a real tap/veth present.
func NewConfiguratorWithMACResolver(interfaces []v1.Interface, networks []v1.Network, resolver MACResolver) *Configurator {
	return &Configurator{interfaces: interfaces, networks: networks, resolveMAC: resolver}
}

// netlinkMACResolver reads the live MAC off the pod-netns interface. This is
// NOT the same as vmi.Spec.Domain.Devices.Interfaces[].MacAddress, which is
// unset for a DRA network: kube-ovn's IPAM (not the VMI author) assigns the
// MAC, and kube-ovn's OVN logical switch port enforces port security keyed on
// that exact MAC. Using anything else — including leaving the domain's <mac>
// unset and letting libvirt invent its own fallback — gets the guest's
// traffic silently dropped at the switch (confirmed live: a libvirt-generated
// 52:54:00:... MAC on the guest's virtio-net device did not match kube-ovn's
// assigned MAC for the same logical port).
func netlinkMACResolver(podIfaceName string) (string, error) {
	link, err := netlink.LinkByName(podIfaceName)
	if err != nil {
		return "", fmt.Errorf("look up %s to read its kube-ovn-assigned MAC: %w", podIfaceName, err)
	}
	mac := link.Attrs().HardwareAddr
	if len(mac) == 0 {
		return "", fmt.Errorf("%s has no hardware address", podIfaceName)
	}
	return mac.String(), nil
}

// PodInterfaceName returns the pod-netns device name this plugin expects for
// a given VMI network. This must match the NicConfig.interfaceName set on the
// ResourceClaim/ResourceClaimTemplate backing that network (see demo/ for an
// example). The result is always podInterfaceNamePrefix + 8 hex chars = 11
// characters, safely under kube-ovn-dra-driver's 12-character budget.
func PodInterfaceName(networkName string) string {
	hash := sha256.Sum256([]byte(networkName))
	return fmt.Sprintf("%s%x", podInterfaceNamePrefix, hash[:podInterfaceNameHashLength/2])
}

// Mutate appends one <interface type="ethernet"> element per plugin-bound
// interface to the domain spec's device list.
//
// Idempotent by construction: KubeVirt's virt-launcher can call OnDefineDomain
// more than once against a domain spec that already carries our interface
// from an earlier call in the same start attempt (observed live: a second,
// unguarded append produced libvirt error "unsupported configuration: non
// unique alias detected: ua-kubeovn0"). Mutate therefore skips any interface
// whose alias it would already add.
func (c *Configurator) Mutate(domainSpec *domainschema.DomainSpec) (*domainschema.DomainSpec, error) {
	networksByName := make(map[string]*v1.Network, len(c.networks))
	for i := range c.networks {
		networksByName[c.networks[i].Name] = &c.networks[i]
	}

	existingAliases := make(map[string]bool, len(domainSpec.Devices.Interfaces))
	for _, existing := range domainSpec.Devices.Interfaces {
		if existing.Alias != nil {
			existingAliases[existing.Alias.GetName()] = true
		}
	}

	for _, iface := range c.interfaces {
		if iface.Binding == nil || iface.Binding.Name != BindingName {
			continue
		}
		if existingAliases[iface.Name] {
			continue // already added by an earlier OnDefineDomain call this attempt
		}

		network, exists := networksByName[iface.Name]
		if !exists {
			return nil, fmt.Errorf("no network found for interface %q", iface.Name)
		}
		if network.NetworkSource.ResourceClaim == nil {
			return nil, fmt.Errorf("network %q for interface %q is not a DRA (resourceClaim) network", network.Name, iface.Name)
		}

		podIfaceName := PodInterfaceName(network.Name)

		mac, err := c.resolveMAC(podIfaceName)
		if err != nil {
			return nil, fmt.Errorf("resolve MAC for interface %q (device %s): %w", iface.Name, podIfaceName, err)
		}

		modelType := iface.Model
		if modelType == "" {
			modelType = v1.VirtIO
		}

		domainIface := domainschema.Interface{
			Alias: domainschema.NewUserDefinedAlias(iface.Name),
			MAC:   &domainschema.MAC{MAC: mac},
			Model: &domainschema.Model{Type: modelType},
			Type:  "ethernet",
			Target: &domainschema.InterfaceTarget{
				Device:  podIfaceName,
				Managed: "no",
			},
		}

		domainSpec.Devices.Interfaces = append(domainSpec.Devices.Interfaces, domainIface)
	}

	return domainSpec, nil
}
