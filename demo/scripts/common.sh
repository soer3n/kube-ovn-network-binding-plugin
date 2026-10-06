#!/usr/bin/env bash

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Shared environment for the demo scripts (build-plugin-image.sh,
# load-images-into-kind.sh, deploy-kubevirt.sh), invoked by the Makefile's
# kind-* targets.

SCRIPTS_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"
REPO_ROOT="$(cd -- "${SCRIPTS_DIR}/../.." &> /dev/null && pwd)"

# The kube-ovn-dra-driver repo checkout this plugin's demo builds on top of.
# Both repos target the same kind cluster (KIND_CLUSTER_NAME below) so the
# driver's own kind-demo prerequisites (kube-ovn, DeviceClass, Subnets) can be
# reused instead of duplicated here.
: ${DRA_DRIVER_DIR:="$(cd -- "${REPO_ROOT}/../kube-ovn-dra-driver" &> /dev/null && pwd)"}

: ${KIND_CLUSTER_NAME:="nic-dra-demo"}

# The registry, image and tag for this plugin's sidecar image. Must match
# manifests/kubevirt-binding-config.yaml's sidecarImage.
: ${PLUGIN_IMAGE_REGISTRY:="ghcr.io/soer3n"}
: ${PLUGIN_IMAGE_NAME:="kube-ovn-network-binding-plugin"}
: ${PLUGIN_IMAGE_TAG:="dev"}
: ${PLUGIN_IMAGE:="${PLUGIN_IMAGE_REGISTRY}/${PLUGIN_IMAGE_NAME}:${PLUGIN_IMAGE_TAG}"}

: ${KUBEVIRT_VERSION:="v1.9.0"}

if [[ -z "${CONTAINER_TOOL}" ]]; then
    if [[ -n "$(which docker)" ]]; then
        CONTAINER_TOOL=docker
    elif [[ -n "$(which podman)" ]]; then
        CONTAINER_TOOL=podman
    else
        echo "No container tool detected. Please install Docker or Podman."
        return 1
    fi
fi
