# Demo: a KubeVirt VMI with a Kube-OVN secondary NIC via DRA

Prerequisites: a cluster with `kube-ovn-dra-driver` installed (its
`DeviceClass nic.kubeovn.io`, created by its Helm chart, and the `ovn-subnet`
Subnet from its own `demo/nic-example/00-kube-ovn-subnets.yaml` — the driver
enumerates kube-ovn Subnets once at startup with no watch, so apply subnets
*before* the driver starts, or restart its DaemonSet afterwards), and
KubeVirt installed with this plugin registered via
`../manifests/kubevirt-binding-config.yaml`.

## The naming contract

There is exactly one thing tying the driver's pod-netns interface to this
plugin's domain XML: **the pod interface name**. It is not discovered at
runtime — it is agreed ahead of time:

1. The VMI network is named `kubeovn0` (`01-vmi.yaml`,
   `spec.networks[].name`).
2. `domain.PodInterfaceName("kubeovn0")` (`sidecar/domain/configurator.go` in
   this repo) computes to `nicdda1950d`.
3. `00-resourceclaimtemplate.yaml` sets `NicConfig.interfaceName:
   nicdda1950d` — this is what tells `kube-ovn-dra-driver` to name the
   pod-netns veth it plumbs exactly that.
4. This plugin's `sidecar/domain/configurator.go` independently recomputes
   the same name from the VMI object it receives in `OnDefineDomain`, and
   points the domain's `<interface type="ethernet">` at that device name.

This is deliberately **not** `kubevirt.io/kubevirt/pkg/network/namescheme`'s
`GenerateHashedInterfaceName` (`"pod"` + 11 hex chars = 14 total). That's
sized for KubeVirt's own IFNAMSIZ budget, but `kube-ovn-dra-driver`'s
`vethNames()` (`pkg/plumbing/plumbing.go`) computes
`containerID[:12-len(ifaceName)]`, so an `ifaceName` over 12 characters
doesn't fit that scheme (it now returns a clear error instead of the panic it
used to — see below). This plugin's own `PodInterfaceName()` stays at `"nic"`
+ 8 hex chars = 11 characters to leave headroom under that limit.

If you rename the VMI network, recompute the name and update the
`ResourceClaimTemplate` — the two sides never talk to each other directly.

## Why a veth alone isn't enough (and what `kube-ovn-dra-driver` does about it)

`kube-ovn-dra-driver` plumbs a **veth**, not a tap device. QEMU's `ethernet`
netdev backend attaches via `TUNSETIFF` on `/dev/net/tun`, which only works
against an actual tun/tap-driver device — not a veth. Verified live: pointing
the domain straight at the veth failed with libvirt errors
(`target managed='no' but specified dev doesn't exist`, then
`Unable to create tap device ...: Invalid argument`).

The fix lives in `kube-ovn-dra-driver`, not here: when it detects the pod is
a KubeVirt `virt-launcher` pod (`kubevirt.io: virt-launcher` label, passed
through the NRI `RunPodSandbox` event), `Attach` additionally renames the
veth out of the way, creates a Linux bridge + a real tap device inside the
pod netns, and enslaves both to it — mirroring KubeVirt's own built-in
`bridge`/`managedTap` binding internally. The tap is named `IfaceName` (the
same contract name above), so **this plugin needed no changes** for that —
its domain XML already targeted the right name; the name just refers to a
tap now instead of a veth.

One more real gotcha, also fixed in the driver: `vishvananda/netlink`'s
`Tuntap` creation doesn't go through rtnetlink — it opens `/dev/net/tun` and
does a `TUNSETIFF` ioctl, which the kernel scopes to the *calling thread's
current netns*, not to any netlink-socket handle. A namespace-scoped
`netlink.Handle` (used correctly for every other link operation in the
driver) silently created the tap in the driver's own host netns instead of
the pod's. The fix explicitly locks the OS thread and switches its netns for
that one call.

## Apply

```bash
kubectl apply -f 00-resourceclaimtemplate.yaml
kubectl apply -f 01-vmi.yaml
```

## Verify

```bash
# pod-side interface present with the expected name — now a tap, enslaved to
# a bridge alongside the renamed original veth
kubectl exec -it <virt-launcher-pod> -c compute -- ip -br link
kubectl exec -it <virt-launcher-pod> -c compute -- bridge link

# domain XML has the ethernet-type interface targeting the tap
kubectl exec -it <virt-launcher-pod> -c compute -- virsh dumpxml <domain> | grep -A5 "type='ethernet'"

# guest connectivity (ping from any in-cluster pod — the kind host itself
# usually cannot route to the pod-network CIDR directly)
kubectl get vmi kube-ovn-dra-vmi -o jsonpath='{.status.interfaces}'

# ssh (Ubuntu containerdisk's default user, key from the ssh_authorized_keys
# in 01-vmi.yaml's cloudInitNoCloud userData)
kubectl virt ssh ubuntu@vmi/kube-ovn-dra-vmi
```
