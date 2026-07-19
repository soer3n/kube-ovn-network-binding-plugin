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

package callback

import (
	"encoding/xml"
	"fmt"

	domainschema "kubevirt.io/kubevirt/pkg/virt-launcher/virtwrap/api"
)

const libvirtDomainQemuSchema = "http://libvirt.org/schemas/domain/qemu/1.0"

// DomainSpecMutator mutates a parsed libvirt domain spec in place.
type DomainSpecMutator interface {
	Mutate(*domainschema.DomainSpec) (*domainschema.DomainSpec, error)
}

// OnDefineDomain unmarshals domainXML, applies mutator, and remarshals it.
// Mirrors kubevirt/vdpa-network-binding-plugin's sidecar/callback package.
func OnDefineDomain(domainXML []byte, mutator DomainSpecMutator) ([]byte, error) {
	domainSpec := &domainschema.DomainSpec{
		XmlNS: libvirtDomainQemuSchema,
	}
	if err := xml.Unmarshal(domainXML, domainSpec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal given domain spec: %w", err)
	}

	updatedDomainSpec, err := mutator.Mutate(domainSpec)
	if err != nil {
		return nil, err
	}

	updatedDomainSpecXML, err := xml.Marshal(updatedDomainSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated domain spec: %w", err)
	}

	return updatedDomainSpecXML, nil
}
