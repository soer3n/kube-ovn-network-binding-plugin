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

# Loads this plugin's sidecar image AND the kube-ovn-dra-driver image into the
# shared kind cluster (KIND_CLUSTER_NAME). The driver image is built+loaded
# by delegating to the sibling kube-ovn-dra-driver repo's own
# `kind-build-driver` target rather than duplicating its build logic here.
#
# Usage: load-images-into-kind.sh

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail

source "${CURRENT_DIR}/common.sh"

echo "Loading plugin image ${PLUGIN_IMAGE} into kind cluster ${KIND_CLUSTER_NAME} ..."
kind load docker-image "${PLUGIN_IMAGE}" --name "${KIND_CLUSTER_NAME}"

if [[ ! -f "${DRA_DRIVER_DIR}/Makefile" ]]; then
    echo "kube-ovn-dra-driver checkout not found at ${DRA_DRIVER_DIR}." >&2
    echo "Set DRA_DRIVER_DIR to the path of that repo checkout." >&2
    exit 1
fi

echo "Building and loading the kube-ovn-dra-driver image via ${DRA_DRIVER_DIR} ..."
make -C "${DRA_DRIVER_DIR}" kind-build-driver KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"
