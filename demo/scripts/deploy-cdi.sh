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

# Installs the CDI (containerized-data-importer) operator + CR, needed by the
# DataVolume demo VM (08-dv-vlan-vm.yaml). The DataVolume uses the cluster's
# default StorageClass (local-path in kind).
#
# Usage: deploy-cdi.sh

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail

source "${CURRENT_DIR}/common.sh"

RELEASE_BASE="https://github.com/kubevirt/containerized-data-importer/releases/download/${CDI_VERSION}"

echo "Installing CDI operator (${CDI_VERSION}) ..."
kubectl apply -f "${RELEASE_BASE}/cdi-operator.yaml"
kubectl wait --for=condition=Established crd/cdis.cdi.kubevirt.io --timeout=120s

echo "Applying the CDI CR (${CDI_VERSION}) ..."
kubectl apply -f "${RELEASE_BASE}/cdi-cr.yaml"

echo "Waiting for CDI to become Available (this can take a few minutes) ..."
kubectl wait --for=condition=Available cdi/cdi --timeout=600s
