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
> DRA (`resourceClaim`) network as of `kubevirt` `v1.9.0-rc.0` — the naming logic
> in `pkg/network/namescheme` is gated to Multus networks only (see
> `kube-ovn-dra-driver`'s [`docs/nic-driver.md`](https://github.com/soer3n/kube-ovn-dra-driver/blob/main/docs/nic-driver.md)
> for that finding and the *possible* upstream fix it's tracking separately).
> This plugin is the interim workaround: a real, working KubeVirt network binding
> plugin, not a patch to KubeVirt core. If that upstream gap ever closes, plain
> `managedTap` could replace this plugin entirely for the overlay/underlay cases
> below — sidecar binding plugins would still be a legitimate KubeVirt extension
> point for anything `managedTap` genuinely can't express, just not required for
> *this specific* problem anymore.

- **Domain wiring (validated end-to-end):** both the OVN overlay case
  (`00`/`01`) and the VLAN underlay case (`03`/`04`) boot, get a real
  DHCP-assigned IP on the DRA-attached secondary NIC, and are reachable —
  see [Examples](#examples) below. The Multus baseline (`02`) is not this
  plugin's code path at all; it exists to isolate plugin/driver-specific bugs
  from anything generic to the OVN pipeline.
- **Sidecar-registration dependency (validated, not assumed):** removing this
  plugin's entry from `KubeVirtConfiguration` doesn't degrade functionality —
  it blocks the VMI from ever getting a pod at all. See [Proof the sidecar is
  actually required](#proof-the-sidecar-is-actually-required) below.
- **Integration test suite (not yet written):** `test/integration/` exists as
  a placeholder; the demo manifests above are currently the only exercised
  path, run by hand against a kind cluster, not in CI.

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
`DomainAttachmentType: managedTap` — but as of `kubevirt` `v1.9.0-rc.0` it
cannot resolve a pod interface name for a DRA network: the naming logic in
`pkg/network/namescheme` only recognizes Multus networks. This plugin routes
around that gap: it runs in **pure sidecar mode** (`SidecarImage` set, no
`DomainAttachmentType`), which makes KubeVirt's core domain configurator skip
the interface entirely and hand 100% of the domain XML construction to this
sidecar's `OnDefineDomain` hook.

## Requirement: kube-ovn with secondary-NIC DRA support

This plugin's demos depend on `kube-ovn-dra-driver`, which in turn depends on
kube-ovn changes **not yet in an upstream release** — DHCP options and a
Logical Switch Port for driver-managed reserved IPs, plus GC-safety fixes so
kube-ovn's garbage collector doesn't reap those ports out from under a
running VM (see `kube-ovn-dra-driver`'s own
[`docs/nic-driver.md`](https://github.com/soer3n/kube-ovn-dra-driver/blob/main/docs/nic-driver.md)
for the full findings). Build/run kube-ovn from the same fork branch the
driver requires:

```
https://github.com/soer3n/kube-ovn   branch: add-dra-support-for-secondary-nics
```

Without it, `01-vmi.yaml`/`04-vlan-vmi.yaml`'s secondary NIC never receives a
DHCP lease and any Logical Switch Port `kube-ovn-dra-driver` creates for it
gets garbage-collected within minutes — the domain-XML wiring this plugin
does succeeds regardless, but the guest never gets usable networking on that
interface.

## How it works

1. `kube-ovn-dra-driver` already supports naming the pod-netns interface it
   creates via a `NicConfig.interfaceName` opaque parameter on the
   `ResourceClaim`/`ResourceClaimTemplate` request config.
2. This plugin and the claim template author agree on that name ahead of
   time by both computing `domain.PodInterfaceName(<VMI network name>)`
   (`sidecar/domain/configurator.go` in this repo) — deliberately **not**
   KubeVirt's own `namescheme.GenerateHashedInterfaceName` (`"pod"` + 11 hex
   = 14 chars): that's sized for KubeVirt's own IFNAMSIZ budget, but
   `kube-ovn-dra-driver`'s `vethNames()` computes
   `containerID[:12-len(ifaceName)]` with a 12-char ceiling, so anything
   over that panicked the driver before it returned a proper error instead.
   This plugin's own scheme (`"nic"` + 8 hex = 11 chars) leaves headroom
   under that limit; see `demo/README.md` for the full explanation.
3. On `OnDefineDomain`, the sidecar finds every VMI interface with
   `binding.name: kube-ovn-nic`, resolves its network, recomputes the same
   hashed name, and appends a libvirt `<interface type="ethernet">
   <target dev="..."/>` element pointing at the interface
   `kube-ovn-dra-driver` already plumbed.

See `demo/README.md` for the naming contract explained in full, and below
for what each demo manifest actually demonstrates.

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
| `03-vlan-resourceclaimtemplate.yaml` + `04-vlan-vmi.yaml` | VLAN underlay (`vlan100-subnet`, tagged onto a `ProviderNetwork`'s physical uplink, no OVN routing hop) | The same DRA + sidecar path against an underlay subnet instead of an overlay one — same driver, same plugin, only the `ResourceClaimTemplate`'s device selector and the VMI's network name change. Because underlay traffic never touches OVN routing, a VM on this subnet is reachable directly by IP from anything else on the same VLAN trunk (e.g. the kind host itself, if it has a matching 802.1q sub-interface) — no NAT, no OVN hop. |
| `02-multus-comparison-vmi.yaml` | OVN overlay, via Multus + KubeVirt's built-in `bridge` binding (`managedTap`) — **not** this plugin | A deliberately non-DRA baseline on the *same* subnet as `01-vmi.yaml`, used to isolate driver/plugin-specific bugs from anything generic to the OVN pipeline or subnet DHCP config. Needs a `NetworkAttachmentDefinition` (kube-ovn CNI, `provider` matching the target Subnet's `spec.provider`) instead of a `ResourceClaimTemplate` — no DRA, no `kube-ovn-dra-driver`, no this plugin. |

`demo/README.md` walks through the naming contract and apply/verify steps
for the overlay case in detail; the VLAN and Multus-comparison manifests
follow the same apply/verify pattern with the CIDR and interface names
swapped in.

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
  Containerfile
manifests/
  kubevirt-binding-config.yaml # KubeVirtConfiguration patch registering this plugin
demo/
  00-resourceclaimtemplate.yaml     # OVN overlay: ResourceClaimTemplate
  01-vmi.yaml                       # OVN overlay: VMI (DRA + this plugin)
  02-multus-comparison-vmi.yaml     # OVN overlay via Multus + managedTap — no DRA, no this plugin
  03-vlan-resourceclaimtemplate.yaml # VLAN underlay: ResourceClaimTemplate
  04-vlan-vmi.yaml                  # VLAN underlay: VMI (DRA + this plugin)
  README.md
```

## Build & test

```bash
make test
make build
make image
```
