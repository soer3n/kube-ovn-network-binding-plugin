# Demo: KubeVirt VMs with Kube-OVN secondary NICs via DRA

Prerequisites:

- kube-ovn with `--enable-dra-nic` (see the driver's README),
- `kube-ovn-dra-driver` installed (its `DeviceClass nic.kubeovn.io`, created
  by its Helm chart, and the subnets from its own
  `demo/nic-example/00-kube-ovn-subnets.yaml`),
- KubeVirt v1.9 with this plugin registered via
  `../manifests/kubevirt-binding-config.yaml` (`make kind-deploy-kubevirt`),
- for `08-dv-vlan-vm.yaml`, CDI and a default StorageClass.

| Files | What it shows |
| --- | --- |
| `00` + `01` (+ `06`) | VMs with a NIC on the OVN overlay subnet `ovn-subnet`; `06` is a second VM for VM-to-VM traffic |
| `03` + `04` (+ `07`) | The same on the VLAN underlay subnet `vlan100-subnet`, reachable from the FRR gateway and the host |
| `03` + `08` | A VM with a CDI DataVolume root disk on `vlan100-subnet`; disk, IP and MAC survive stop/start |
| `02` | Baseline without DRA: the same subnet through Multus and KubeVirt's `bridge` binding |
| `05` | An SSH bastion pod for reaching the VMs by their in-cluster IPs |

The VM manifests embed a demo SSH public key in their cloud-init; replace it
with your own.

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

`PodInterfaceName()` is `"nic"` + 8 hex chars = 11 characters, within the 15
characters (IFNAMSIZ) the driver accepts for `NicConfig.interfaceName`.

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
same contract name above), so the domain XML targets the tap. The driver
also runs a small DHCP server on that bridge, which hands the guest the
IPv4 address kube-ovn-controller allocated for the NIC.

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
kubectl apply -f 00-resourceclaimtemplate.yaml -f 01-vmi.yaml -f 06-vmi-2.yaml
kubectl apply -f 03-vlan-resourceclaimtemplate.yaml -f 04-vlan-vmi.yaml -f 07-vlan-vmi-2.yaml
kubectl apply -f 08-dv-vlan-vm.yaml   # needs CDI
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
