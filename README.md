# kube-ovn-network-binding-plugin

A KubeVirt [network binding
plugin](https://kubevirt.io/user-guide/network/network_binding_plugins/) hook
sidecar that attaches a
[`kube-ovn-dra-driver`](https://github.com/soer3n/kube-ovn-dra-driver)-managed
secondary NIC to a VM domain.

## Status & scope

> **This is an exploration, not a product.** It exists because `kube-ovn-dra-driver`
> proved DRA + NRI can wire a Kube-OVN secondary NIC into a pod's network
> namespace with no Multus involved — but a KubeVirt VM's domain (libvirt/QEMU)
> never knew that interface existed. KubeVirt's own sidecar-free path for that,
> `DomainAttachmentType: managedTap`, cannot resolve a pod interface name for a
> DRA (`resourceClaim`) network as of KubeVirt `v1.9.0` and `v1.10.0-alpha.0` —
> the naming logic in `pkg/network/namescheme` only handles Multus networks.
> This plugin is the interim workaround: a real, working KubeVirt network binding
> plugin, not a patch to KubeVirt core. If that upstream gap ever closes, plain
> `managedTap` could replace this plugin entirely for the overlay/underlay cases
> below — sidecar binding plugins would still be a legitimate KubeVirt extension
> point for anything `managedTap` genuinely can't express, just not required for
> *this specific* problem anymore.

- **Domain wiring (validated end-to-end):** both the OVN overlay case
  (`00`/`01`/`06`) and the VLAN underlay case (`03`/`04`/`07`/`08`) boot, get a
  real DHCP-assigned IP on the DRA-attached secondary NIC, keep IP and MAC
  across VM restarts, and are reachable — see [Examples](#examples) below. The Multus baseline (`02`) is not this
  plugin's code path at all; it exists to isolate plugin/driver-specific bugs
  from anything generic to the OVN pipeline.
- **Sidecar-registration dependency (validated, not assumed):** removing this
  plugin's entry from `KubeVirtConfiguration` doesn't degrade functionality —
  it blocks the VMI from ever getting a pod at all. See [Proof the sidecar is
  actually required](#proof-the-sidecar-is-actually-required) below.
- **Tests:** unit tests cover the domain configurator, the hook server and
  the domain XML callback (`make test`). The end-to-end path is exercised with
  the demo manifests against a kind cluster, not in CI.

## Why this exists

`kube-ovn-dra-driver` gives *pods* a Kube-OVN secondary NIC via Kubernetes
Dynamic Resource Allocation (DRA) + NRI, entirely independent of Multus. That
plumbs a fully IP/MAC/MTU-configured veth into the pod network namespace, but
nothing about it tells a KubeVirt VM's domain (libvirt/QEMU) to actually use
that interface — Multus-attached interfaces get that wiring from KubeVirt's
core network setup and admission is already alpha-gated for DRA
(`resourceClaim`) networks via the `NetworkDevicesWithDRA` feature gate and
VEP-183, but *something* still has to turn the pod-side interface into a
domain-visible NIC.

KubeVirt has a built-in, sidecar-free way to do that —
`DomainAttachmentType: managedTap` — but as of KubeVirt `v1.9.0` it
cannot resolve a pod interface name for a DRA network: the naming logic in
`pkg/network/namescheme` only recognizes Multus networks. This plugin routes
around that gap: it runs in **pure sidecar mode** (`SidecarImage` set, no
`DomainAttachmentType`), which makes KubeVirt's core domain configurator skip
the interface entirely and hand 100% of the domain XML construction to this
sidecar's `OnDefineDomain` hook.

## Requirement: kube-ovn with DRA NIC support

The demos depend on `kube-ovn-dra-driver`, which in turn needs kube-ovn with
the experimental `--enable-dra-nic` flag: kube-ovn-controller then allocates
address and MAC for every NIC the driver's claims request, creates its logical
switch port, and keeps IP and MAC of a VM's NICs across restarts. That support is proposed upstream and not in a kube-ovn release yet;
until then use branch `dra-nic-upstream` of
[github.com/soer3n/kube-ovn](https://github.com/soer3n/kube-ovn). See the
driver's [README](https://github.com/soer3n/kube-ovn-dra-driver#requirements).

Without it, the VM's secondary NIC never gets an address — the domain-XML
wiring this plugin does succeeds regardless, but the guest never gets usable
networking on that interface.

## How it works

1. `kube-ovn-dra-driver` already supports naming the pod-netns interface it
   creates via a `NicConfig.interfaceName` opaque parameter on the
   `ResourceClaim`/`ResourceClaimTemplate` request config.
2. This plugin and the claim template author agree on that name ahead of
   time by both computing `domain.PodInterfaceName(<VMI network name>)`
   (`sidecar/domain/configurator.go` in this repo): `"nic"` + 8 hex = 11
   characters, within the 15 characters the driver accepts for an interface
   name.
3. On `OnDefineDomain`, the sidecar finds every VMI interface with
   `binding.name: kube-ovn-nic`, resolves its network, recomputes the same
   hashed name, reads the MAC kube-ovn assigned from that pod interface, and
   appends a libvirt `<interface type="ethernet"> <target dev="..."/>`
   element pointing at the tap `kube-ovn-dra-driver` already plumbed.

See `demo/README.md` for the naming contract explained in full, and below
for what each demo manifest actually demonstrates.

## Migrating to `managedTap` later

This plugin is a workaround. With `domainAttachmentType: managedTap`, KubeVirt
creates the tap and the domain interface itself, which would remove

- this sidecar and its container in every `virt-launcher` pod,
- the bridge, tap and DHCP server the driver adds to VM pods,

and leave the tap wiring to KubeVirt's own, maintained code. It needs a
KubeVirt release that resolves pod interface names for DRA networks in
`pkg/network/namescheme`; `v1.9.0` and `v1.10.0-alpha.0` only handle Multus
networks there.

`managedTap` alone keeps a naming contract: the claim template would carry
the name KubeVirt's namescheme expects instead of `domain.PodInterfaceName()`.
The contract would go away if the driver reported the NIC's interface name,
MAC and IPs in the ResourceClaim status (`status.devices[].networkData`) and
KubeVirt read them from there. Whether KubeVirt's DRA network design (VEP-183)
plans for that is still to be checked; it is the upstream change to propose.

The move is not tested yet; expect these steps:

1. **Driver:** `kube-ovn-dra-driver` currently wraps the NIC of every
   `virt-launcher` pod in its own bridge, tap and DHCP server. With
   `managedTap`, KubeVirt needs the plain veth, so the driver needs an option
   to skip that wiring first (not implemented yet).
2. **Binding registration:** replace `sidecarImage` with
   `domainAttachmentType: managedTap` in `manifests/kubevirt-binding-config.yaml`.
3. **Interface names:** set `NicConfig.interfaceName` in the
   `ResourceClaimTemplate`s to the name KubeVirt's namescheme generates for the
   network instead of `domain.PodInterfaceName()`.
4. **Guest addressing:** the driver's DHCP server goes away, so the guest needs
   another source, e.g. kube-ovn's DHCP on the subnet (`enableDHCP`).
5. **Restart the VMs**, so their pods are recreated. kube-ovn keeps IP and MAC,
   since it names the ports after the VM. Exception: when several NICs of a VM
   share a provider, kube-ovn keys them by `<provider>.<interfaceName>`, so a
   renamed interface gets a new port and address.

## Examples

All of these need `kube-ovn-dra-driver` installed and running — it's what
actually plumbs the veth/tap/bridge into the pod network namespace and
serves DHCP to the guest; this plugin only tells libvirt which device to
attach to the domain. See that repo's own `demo/nic-example/` for the
`DeviceClass`, `Subnet`, and (for the VLAN case) `ProviderNetwork`/`Vlan`
prerequisites each scenario below assumes are already applied.

| Files | Network type | What it shows |
| --- | --- | --- |
| `00-resourceclaimtemplate.yaml` + `01-vmi.yaml` | OVN overlay (`ovn-subnet`, routed through `br-int`/OVN) | The primary DRA + sidecar path: a VMI gets a Kube-OVN secondary NIC with no Multus at all — DRA allocates the device, `kube-ovn-dra-driver`'s NRI hook wires the dataplane, this plugin wires the domain. |
| `06-vmi-2.yaml` | OVN overlay | A second VM sharing `00`'s claim template, for VM-to-VM traffic over the DRA NIC. |
| `03-vlan-resourceclaimtemplate.yaml` + `04-vlan-vmi.yaml` (+ `07-vlan-vmi-2.yaml`) | VLAN underlay (`vlan100-subnet`, bridged by kube-ovn's localnet port onto a `ProviderNetwork`'s physical uplink) | The same DRA + sidecar path against an underlay subnet instead of an overlay one — same driver, same plugin, only the `ResourceClaimTemplate`'s device selector and the VMI's network name change. A VM on this subnet is reachable by IP from anything else on the same VLAN, without NAT. |
| `08-dv-vlan-vm.yaml` | VLAN underlay | A VM with a CDI DataVolume root disk; disk contents, IP and MAC survive stop/start. Needs CDI. |
| `05-ssh-bastion.yaml` | — | An SSH bastion pod for reaching the VMs by their in-cluster IPs. |
| `02-multus-comparison-vmi.yaml` | OVN overlay, via Multus + KubeVirt's built-in `bridge` binding (`managedTap`) — **not** this plugin | A deliberately non-DRA baseline on the *same* subnet as `01-vmi.yaml`, used to isolate driver/plugin-specific bugs from anything generic to the OVN pipeline or subnet DHCP config. Needs a `NetworkAttachmentDefinition` (kube-ovn CNI, `provider` matching the target Subnet's `spec.provider`) instead of a `ResourceClaimTemplate` — no DRA, no `kube-ovn-dra-driver`, no this plugin. |

`demo/README.md` walks through the naming contract and apply/verify steps
for the overlay case in detail; the VLAN and Multus-comparison manifests
follow the same apply/verify pattern with the CIDR and interface names
swapped in. The VM manifests embed a demo SSH public key in their cloud-init;
replace it with your own.

### Proof the sidecar is actually required

Removing the `kube-ovn-nic` entry from
`KubeVirt.spec.configuration.network.binding` (e.g. `kubectl patch kubevirt
kubevirt -n kubevirt --type json -p '[{"op":"remove","path":"/spec/configuration/network/binding/kube-ovn-nic"}]'`)
and then creating a VMI that references it doesn't fail at admission — the
admission webhook only checks that the interface has `Binding` or `SRIOV`
set, not that the binding name is registered anywhere. It fails later,
inside `virt-controller`, when building the pod spec: `netbinding.NetBindingPluginSidecarList`
(`kubevirt.io/kubevirt` `pkg/network/netbinding/netbinding.go`) can't resolve
`iface.Binding.Name` against the live `KubeVirtConfiguration`, and that
error aborts the *entire* pod-manifest render — pod-spec construction is
atomic, so a single unresolvable binding means no pod at all, not a
degraded one:

```
status.conditions:
  reason: FailedCreate
  message: 'failed to render launch manifest: couldn''t find configuration for network binding: kube-ovn-nic'
```

Restoring the binding (`kubectl apply -f manifests/kubevirt-binding-config.yaml`)
makes the *same* VMI object self-heal — no delete/recreate needed; a pod
appears within seconds and boots normally. This is why this plugin isn't
optional convenience: without it registered, `kube-ovn-dra-driver`'s
otherwise-complete dataplane wiring never gets a VM to attach to at all.

## Layout

```
sidecar/
  main.go             # gRPC bootstrap: registers Info + Callbacks services on the shared hook socket
  server/server.go     # Info() and OnDefineDomain()/PreCloudInitIso()/Shutdown() implementations
  callback/callback.go # generic unmarshal -> mutate -> remarshal helper for the domain XML
  domain/configurator.go # the actual interface-construction logic
  Dockerfile
manifests/
  kubevirt-binding-config.yaml # KubeVirtConfiguration patch registering this plugin
demo/
  00-resourceclaimtemplate.yaml      # OVN overlay: ResourceClaimTemplate
  01-vmi.yaml, 06-vmi-2.yaml         # OVN overlay: VMs (DRA + this plugin)
  02-multus-comparison-vmi.yaml      # OVN overlay via Multus + bridge binding — no DRA, no this plugin
  03-vlan-resourceclaimtemplate.yaml # VLAN underlay: ResourceClaimTemplate
  04-vlan-vmi.yaml, 07-vlan-vmi-2.yaml # VLAN underlay: VMs (DRA + this plugin)
  08-dv-vlan-vm.yaml                 # VLAN underlay: VM with a DataVolume root disk
  05-ssh-bastion.yaml                # SSH bastion pod
  scripts/                           # image build and KubeVirt deployment for kind
  README.md
```

## Install

Register the released sidecar image (`docker.io/soer3n/kube-ovn-network-binding-plugin:v0.1.0`)
and the `NetworkDevicesWithDRA` feature gate in KubeVirt:

```bash
kubectl patch kubevirt kubevirt -n kubevirt --type merge \
  --patch-file manifests/kubevirt-binding-config.yaml
```

The kind demo (`make kind-deploy-kubevirt`) registers the locally built
`:dev` image instead.

## Build & test

```bash
make test
make build
make image    # docker.io/soer3n/kube-ovn-network-binding-plugin:dev
```

CI lints and tests every push and pull request. `.github/workflows/image.yaml`
builds the sidecar image for `linux/amd64` and `linux/arm64` and pushes it to
`docker.io/soer3n/kube-ovn-network-binding-plugin` on pushes to `main` (tags
`main`, `sha-<commit>`) and on `v*` tags (`v<version>`, `v<major>.<minor>`,
`latest`). Pushing needs the repository secrets `DOCKERHUB_USERNAME` and
`DOCKERHUB_TOKEN`.
