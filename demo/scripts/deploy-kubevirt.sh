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

# Installs the KubeVirt operator + CR from the same pre-release tag this
# repo's go.mod is pinned to (KUBEVIRT_VERSION, see common.sh) — that tag is
# the first to publish the ResourceClaim network fields this plugin depends
# on. Then patches in this plugin's binding registration + the
# NetworkDevicesWithDRA feature gate.
#
# Usage: deploy-kubevirt.sh

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail

source "${CURRENT_DIR}/common.sh"

RELEASE_BASE="https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}"

echo "Installing KubeVirt operator (${KUBEVIRT_VERSION}) ..."
kubectl apply -f "${RELEASE_BASE}/kubevirt-operator.yaml"

echo "Waiting for the KubeVirt CRD to be established ..."
kubectl wait --for=condition=Established crd/kubevirts.kubevirt.io --timeout=120s

echo "Applying the KubeVirt CR (${KUBEVIRT_VERSION}) ..."
kubectl apply -f "${RELEASE_BASE}/kubevirt-cr.yaml"

echo "Registering ${PLUGIN_IMAGE_NAME} binding + NetworkDevicesWithDRA feature gate ..."
kubectl patch kubevirt kubevirt -n kubevirt --type merge \
    --patch-file "${REPO_ROOT}/manifests/kubevirt-binding-config.yaml"

echo "Waiting for KubeVirt to become Available (this can take a few minutes) ..."
kubectl wait --for=condition=Available -n kubevirt kv/kubevirt --timeout=300s
