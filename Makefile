IMAGE ?= docker.io/soer3n/kube-ovn-network-binding-plugin
TAG ?= dev

SCRIPTS_DIR       ?= $(CURDIR)/demo/scripts
KIND_CLUSTER_NAME ?= nic-dra-demo
DRA_DRIVER_DIR    ?= $(CURDIR)/../kube-ovn-dra-driver

.PHONY: build test vet fmt image push clean \
        kind-build-plugin-image kind-load-images kind-deploy-kubevirt \
        kind-deploy-demo-vmi kind-demo kind-delete kind-status

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

image:
	docker build -f sidecar/Dockerfile -t $(IMAGE):$(TAG) .

push: image
	docker push $(IMAGE):$(TAG)

clean:
	rm -f kube-ovn-network-binding-sidecar

## kind-build-plugin-image: build this plugin's sidecar image (see sidecar/Dockerfile).
kind-build-plugin-image:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) $(SCRIPTS_DIR)/build-plugin-image.sh

## kind-load-images: load the plugin image AND the kube-ovn-dra-driver image
## (built via $(DRA_DRIVER_DIR)'s own kind-build-driver target) into the
## shared kind cluster.
kind-load-images: kind-build-plugin-image
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) DRA_DRIVER_DIR=$(DRA_DRIVER_DIR) $(SCRIPTS_DIR)/load-images-into-kind.sh

## kind-deploy-kubevirt: install the KubeVirt operator+CR pinned to this
## repo's go.mod version, then patch in the binding registration and the
## NetworkDevicesWithDRA feature gate.
kind-deploy-kubevirt:
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) $(SCRIPTS_DIR)/deploy-kubevirt.sh

## kind-deploy-demo-vmi: apply the demo ResourceClaimTemplate + VMI.
kind-deploy-demo-vmi:
	kubectl apply -f demo/00-resourceclaimtemplate.yaml
	kubectl apply -f demo/01-vmi.yaml
	@echo "Waiting for the demo VMI to be Running ..."
	kubectl wait --for=jsonpath='{.status.phase}'=Running vmi/kube-ovn-dra-vmi --timeout=180s

## kind-demo: build the base cluster (kind, kube-ovn, DeviceClass/subnets,
## the NIC driver) via the sibling kube-ovn-dra-driver repo, then layer
## KubeVirt + this plugin + the demo VMI on top. Does NOT deploy Multus or
## the VLAN/containerlab topology — the demo VMI only needs the OVN overlay
## subnet ("ovn-subnet"), already part of kube-ovn-dra-driver's nic-prereqs.
kind-demo:
	$(MAKE) -C $(DRA_DRIVER_DIR) kind-create kind-deploy-kube-ovn kind-deploy-nic-prereqs \
		kind-build-driver kind-deploy-driver KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME)
	$(MAKE) kind-deploy-kubevirt
	$(MAKE) kind-load-images
	$(MAKE) kind-deploy-demo-vmi
	@echo ""
	@echo "=== kube-ovn DRA + KubeVirt binding-plugin demo is running ==="
	@echo "Cluster:  $(KIND_CLUSTER_NAME)"
	@echo "VMI:      kubectl get vmi kube-ovn-dra-vmi"
	@echo "Console:  virtctl console kube-ovn-dra-vmi"
	@echo "Teardown: make kind-delete"

## kind-status: show the demo VMI, its pod, and interfaces for a quick check.
kind-status:
	@echo "=== VMI ==="
	kubectl get vmi kube-ovn-dra-vmi -o wide
	@echo ""
	@echo "=== virt-launcher pod ==="
	kubectl get pod -l kubevirt.io/created-by --field-selector=status.phase=Running -o wide
	@echo ""
	@echo "=== VMI network interfaces (status) ==="
	kubectl get vmi kube-ovn-dra-vmi -o jsonpath='{.status.interfaces}' | python3 -m json.tool

## kind-delete: destroy the shared demo cluster (delegates to kube-ovn-dra-driver).
kind-delete:
	$(MAKE) -C $(DRA_DRIVER_DIR) kind-delete KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME)
