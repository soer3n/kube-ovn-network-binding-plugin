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

package callback_test

import (
	"encoding/xml"
	"errors"
	"testing"

	domainschema "kubevirt.io/kubevirt/pkg/virt-launcher/virtwrap/api"

	"github.com/soer3n/kube-ovn-network-binding-plugin/sidecar/callback"
)

type mutatorFunc func(*domainschema.DomainSpec) (*domainschema.DomainSpec, error)

func (f mutatorFunc) Mutate(spec *domainschema.DomainSpec) (*domainschema.DomainSpec, error) {
	return f(spec)
}

func TestOnDefineDomain(t *testing.T) {
	rename := mutatorFunc(func(spec *domainschema.DomainSpec) (*domainschema.DomainSpec, error) {
		spec.Name = "mutated"
		return spec, nil
	})
	out, err := callback.OnDefineDomain([]byte("<domain type='kvm'><name>test</name></domain>"), rename)
	if err != nil {
		t.Fatalf("OnDefineDomain() error = %v", err)
	}
	spec := &domainschema.DomainSpec{}
	if err := xml.Unmarshal(out, spec); err != nil {
		t.Fatalf("result is not a domain: %v", err)
	}
	if spec.Name != "mutated" || spec.Type != "kvm" {
		t.Errorf("result = name %q type %q, want the mutated kvm domain", spec.Name, spec.Type)
	}

	if _, err := callback.OnDefineDomain([]byte("<domain"), rename); err == nil {
		t.Error("OnDefineDomain() with invalid XML: want error")
	}

	failing := mutatorFunc(func(*domainschema.DomainSpec) (*domainschema.DomainSpec, error) {
		return nil, errors.New("mutate failed")
	})
	if _, err := callback.OnDefineDomain([]byte("<domain/>"), failing); err == nil || err.Error() != "mutate failed" {
		t.Errorf("OnDefineDomain() with a failing mutator error = %v, want the mutator's error", err)
	}
}
