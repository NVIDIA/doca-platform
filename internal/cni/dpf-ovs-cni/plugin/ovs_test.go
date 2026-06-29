/*
Copyright 2026 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plugin

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"testing"

	"github.com/nvidia/doca-platform/pkg/ovsmodel"
	"github.com/nvidia/doca-platform/pkg/ovsutils"

	current "github.com/containernetworking/cni/pkg/types/100"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"go.uber.org/mock/gomock"
)

var showAllCollisions = flag.Bool("show-all-collisions", false, "log every hash collision, not just the total")

// ---------- hashToOFPort tests ----------

// TestHashToOFPort_RangeIsValid verifies that hashToOFPort always returns a
// port number within the controller-owned range [minOFPort, maxOFPort] for
// realistic DPU interface names and edge cases.
func TestHashToOFPort_RangeIsValid(t *testing.T) {
	names := []string{
		// Physical ports
		"p0", "p1",
		// Host PF representors
		"pf0hpf", "pf1hpf",
		// VF representors
		"pf0vf0", "pf0vf1", "pf0vf15",
		// SF representors
		"en3f0pf0sf51", "en3f0pf0sf47", "en3f0pf0sf64",
		// Patch ports
		"pen3f0pf0sf51brsfc", "pen3f0pf0sf47brhbn", "pen3f0pf0sf64brhbn",
		// Edge cases
		"", "a",
	}
	for _, name := range names {
		port := hashToOFPort(name)
		if port < minOFPort || port > maxOFPort {
			t.Errorf("hashToOFPort(%q) = %d, want in [%d, %d]", name, port, minOFPort, maxOFPort)
		}
	}
}

// TestHashToOFPort_Deterministic ensures the hash is pure: the same interface
// name always maps to the same port number across repeated calls.
func TestHashToOFPort_Deterministic(t *testing.T) {
	name := "en3f0pf0sf51"
	first := hashToOFPort(name)
	for i := 0; i < 100; i++ {
		if got := hashToOFPort(name); got != first {
			t.Fatalf("hashToOFPort(%q) returned %d on iteration %d, expected %d", name, got, i, first)
		}
	}
}

// ---------- resolveOFPort tests ----------

// TestResolveOFPort_NoCollision verifies that when no ports are in use,
// resolveOFPort returns the hash-derived candidate directly without probing.
func TestResolveOFPort_NoCollision(t *testing.T) {
	name := "en3f0pf0sf51"
	used := map[uint]bool{}
	port := resolveOFPort(name, used)
	expected := hashToOFPort(name)
	if port != expected {
		t.Errorf("resolveOFPort with empty used map: got %d, want hash candidate %d", port, expected)
	}
	if port < minOFPort || port > maxOFPort {
		t.Errorf("resolveOFPort returned %d, want in [%d, %d]", port, minOFPort, maxOFPort)
	}
}

// TestResolveOFPort_SkipsUsedCandidate verifies that when the hash candidate is
// already taken, resolveOFPort linear-probes forward and returns a free slot.
func TestResolveOFPort_SkipsUsedCandidate(t *testing.T) {
	name := "pen3f0pf0sf47brhbn"
	candidate := hashToOFPort(name)
	used := map[uint]bool{candidate: true}

	port := resolveOFPort(name, used)
	if port == candidate {
		t.Fatalf("resolveOFPort should not return the used candidate %d", candidate)
	}
	if port < minOFPort || port > maxOFPort {
		t.Errorf("resolveOFPort returned %d, want in [%d, %d]", port, minOFPort, maxOFPort)
	}
	if used[port] {
		t.Errorf("resolveOFPort returned %d which is already in the used set", port)
	}
}

// TestResolveOFPort_SkipsMultipleUsed verifies that linear probing correctly
// walks past a contiguous run of occupied slots (candidate + 10 consecutive
// probes blocked) and returns the first free slot after the run.
func TestResolveOFPort_SkipsMultipleUsed(t *testing.T) {
	name := "pen3f0pf0sf64brsfc"
	candidate := hashToOFPort(name)

	used := make(map[uint]bool)
	for i := uint(0); i <= 10; i++ {
		p := minOFPort + (candidate-minOFPort+i)%oFPortCount
		used[p] = true
	}

	port := resolveOFPort(name, used)
	if used[port] {
		t.Fatalf("resolveOFPort returned %d which is in the used set", port)
	}
	if port < minOFPort || port > maxOFPort {
		t.Errorf("resolveOFPort returned %d, want in [%d, %d]", port, minOFPort, maxOFPort)
	}
	expected := minOFPort + (candidate-minOFPort+11)%oFPortCount
	if port != expected {
		t.Errorf("resolveOFPort = %d, want %d (first free after 11 blocked slots)", port, expected)
	}
}

// TestResolveOFPort_WrapsAround verifies that when every port from the
// candidate up to maxOFPort is occupied, the probe wraps around to minOFPort
// and returns the first free slot at the beginning of the range.
func TestResolveOFPort_WrapsAround(t *testing.T) {
	name := "en3f0pf0sf64"
	candidate := hashToOFPort(name)

	used := make(map[uint]bool)
	for p := candidate; p <= maxOFPort; p++ {
		used[p] = true
	}

	port := resolveOFPort(name, used)
	if used[port] {
		t.Fatalf("resolveOFPort returned %d which is in the used set", port)
	}
	if port < minOFPort || port > maxOFPort {
		t.Errorf("resolveOFPort returned %d, want in [%d, %d]", port, minOFPort, maxOFPort)
	}
	if port != minOFPort {
		t.Errorf("resolveOFPort = %d, want %d (first slot after wrap)", port, minOFPort)
	}
}

// TestResolveOFPort_AllSlotsExhausted verifies that when every port in
// [minOFPort, maxOFPort] is occupied, resolveOFPort returns 0 as a sentinel.
// Callers treat 0 as "omit ofport_request" and let OVS auto-assign.
func TestResolveOFPort_AllSlotsExhausted(t *testing.T) {
	used := make(map[uint]bool)
	for p := minOFPort; p <= maxOFPort; p++ {
		used[p] = true
	}

	port := resolveOFPort("en3f0pf0sf51", used)
	if port != 0 {
		t.Errorf("resolveOFPort with all slots used: got %d, want 0 (sentinel)", port)
	}
}

// TestResolveOFPort_DifferentNamesGetDifferentPorts simulates sequential port
// allocation for multiple SF and patch interfaces on the same bridge. Each
// resolved port is added to the used set before the next resolve, mirroring
// real createPort/addPatchPort behavior. Verifies no two interfaces collide.
func TestResolveOFPort_DifferentNamesGetDifferentPorts(t *testing.T) {
	used := make(map[uint]bool)
	names := []string{
		"p0", "p1",
		"pf0hpf", "pf1hpf",
		"pf0vf0", "pf0vf3",
		"en3f0pf0sf47", "en3f0pf0sf51", "en3f0pf0sf64",
		"pen3f0pf0sf47brsfc", "pen3f0pf0sf51brhbn",
	}
	ports := make([]uint, len(names))

	for i, name := range names {
		port := resolveOFPort(name, used)
		if port == 0 {
			t.Fatalf("resolveOFPort(%q) returned sentinel 0 unexpectedly", name)
		}
		for j := 0; j < i; j++ {
			if ports[j] == port {
				t.Errorf("resolveOFPort(%q) = %d collides with resolveOFPort(%q) = %d",
					name, port, names[j], ports[j])
			}
		}
		ports[i] = port
		used[port] = true
	}
}

// TestResolveOFPort_TwoCollidingNames simulates two interfaces whose names
// hash to the same candidate port. The first call gets the candidate; the
// second call (same name, candidate now marked used) must linear-probe to
// candidate+1. This is the core collision-avoidance scenario the commit fixes.
func TestResolveOFPort_TwoCollidingNames(t *testing.T) {
	name := "en3f0pf0sf51"
	candidate := hashToOFPort(name)

	used := make(map[uint]bool)
	port1 := resolveOFPort(name, used)
	if port1 != candidate {
		t.Fatalf("expected first resolve to return candidate %d, got %d", candidate, port1)
	}
	used[port1] = true

	// Call again with the same name (same hash) while candidate is taken.
	port2 := resolveOFPort(name, used)
	if port2 == port1 {
		t.Fatalf("second resolve should not return the same port %d", port1)
	}
	if port2 < minOFPort || port2 > maxOFPort {
		t.Errorf("resolveOFPort returned %d, want in [%d, %d]", port2, minOFPort, maxOFPort)
	}
	expected := minOFPort + (candidate-minOFPort+1)%oFPortCount
	if port2 != expected {
		t.Errorf("resolveOFPort = %d, want %d (next linear probe)", port2, expected)
	}
}

// TestHashToOFPort_ReportCollisions enumerates valid DPU interface names for
// several deployment profiles and reports hash collisions from hashToOFPort.
//
// Port name ranges per profile:
//   - Physical ports:     p0–p1
//   - Host PF reps:       pf{0..maxPF}hpf
//   - VF reps:            pf{0..maxPF}vf{0..maxVFIndex}
//   - SF reps:            en3f{N}pf{N}sf{0..maxSFIndex}  (f and pf indices match the ECPF/port)
//   - Patch ports (SF):   pen3f{N}pf{N}sf{0..maxSFIndex}brsfc / brhbn
//
// Profiles:
//
//	Name                 | maxPF | ECPFs with VFs/SFs | VFs/ECPF | SFs/ECPF | Total names
//	---------------------+-------+--------------------+----------+----------+------------
//	DPFFlavor            |   1   |       ECPF0        |   0–46   |   0–20   |        115
//	DPFFlavorExtended    |   1   |    ECPF0–ECPF1     |  0–125   |  0–110   |        922
//	AllPorts             |   3   |    ECPF0–ECPF3     |  0–511   |  0–1025  |     14,366
//
// Sample names generated for each port type:
//
//	Port type   | DPFFlavor                   | DPFFlavorExtended            | AllPorts
//	------------+-----------------------------+------------------------------+------------------------------------
//	Physical    | p0, p1                      | p0, p1                       | p0, p1
//	Host PF rep | pf0hpf, pf1hpf              | pf0hpf, pf1hpf               | pf0hpf … pf3hpf
//	VF rep      | pf0vf0 … pf0vf46            | pf0vf0 … pf0vf125, pf1vf0 … pf1vf125 | pf0vf0 … pf0vf511 … pf3vf0 … pf3vf511
//	SF rep      | en3f0pf0sf0 … sf20          | en3f0pf0sf0 … sf110, en3f1pf1sf0 … sf110 | en3f0pf0sf0 … sf1025 … en3f3pf3sf0 … sf1025
//	Patch brsfc | pen3f0pf0sf0brsfc … sf20    | pen3f0pf0sf0brsfc … sf110, pen3f1pf1sf0brsfc … sf110 | pen3f0pf0sf0brsfc … sf1025
//	Patch brhbn | pen3f0pf0sf0brhbn … sf20    | pen3f0pf0sf0brhbn … sf110, pen3f1pf1sf0brhbn … sf110 | pen3f0pf0sf0brhbn … sf1025
//
// Observed results (oFPortCount=32512, range 32768–65279):
//
//	Name                 | Names | Collisions | Unique ofports | Collision rate
//	---------------------+-------+------------+----------------+---------------
//	DPFFlavor            |   114 |          0 |            114 |          0.0%
//	DPFFlavorExtended    |   922 |         12 |            910 |          1.3%
//	AllPorts             |14,366 |      2,857 |         11,509 |         19.9%
//
// DPFFlavorExtended collisions (12):
//
//	Name A                    | Name B                   | ofport
//	--------------------------+--------------------------+-------
//	pf1vf50                   | pf1vf25                  | 58887
//	pf1vf51                   | pf1vf24                  | 57460
//	pf1vf52                   | pf1vf27                  | 61741
//	pf1vf53                   | pf1vf26                  | 60314
//	pf1vf88                   | pf1vf35                  | 47082
//	pf1vf89                   | pf1vf34                  | 48509
//	pen3f0pf0sf73brhbn        | pen3f0pf0sf17brsfc       | 60920
//	en3f0pf0sf88              | en3f0pf0sf13             | 52688
//	pen3f1pf1sf26brsfc        | pf0vf41                  | 51522
//	pen3f1pf1sf28brhbn        | pf0vf12                  | 32814
//	pen3f1pf1sf42brsfc        | pf0vf87                  | 54716
//	en3f1pf1sf109             | pen3f1pf1sf42brhbn       | 61110
//
// Usage:
//
//	go test -v -run TestHashToOFPort_ReportCollisions
//	go test -v -run TestHashToOFPort_ReportCollisions/DPFFlavor
//	go test -v -run TestHashToOFPort_ReportCollisions -args -show-all-collisions
func TestHashToOFPort_ReportCollisions(t *testing.T) {
	tests := []struct {
		name       string
		maxPF      int // host PF representors: pf{0..maxPF}hpf
		maxECPF    int // ECPFs that carry VFs and SFs: 0..maxECPF
		maxVFIndex int
		maxSFIndex int
	}{
		{"DPFFlavor", 1, 0, 46, 20},
		{"DPFFlavorExtended", 1, 1, 125, 110},
		{"AllPorts", 3, 3, 511, 1025},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var names []string

			// Physical ports
			for i := 0; i <= 1; i++ {
				names = append(names, fmt.Sprintf("p%d", i))
			}
			// Host PF representors
			for pf := 0; pf <= tc.maxPF; pf++ {
				names = append(names, fmt.Sprintf("pf%dhpf", pf))
			}
			// VF representors (only on ECPFs 0..maxECPF)
			for pf := 0; pf <= tc.maxECPF; pf++ {
				for vf := 0; vf <= tc.maxVFIndex; vf++ {
					names = append(names, fmt.Sprintf("pf%dvf%d", pf, vf))
				}
			}
			// SF representors + patch ports (f index matches pf index per ECPF)
			for pf := 0; pf <= tc.maxECPF; pf++ {
				for sf := 0; sf <= tc.maxSFIndex; sf++ {
					sfName := fmt.Sprintf("en3f%dpf%dsf%d", pf, pf, sf)
					names = append(names, sfName)
					names = append(names, fmt.Sprintf("p%sbrsfc", sfName))
					names = append(names, fmt.Sprintf("p%sbrhbn", sfName))
				}
			}

			t.Logf("generated %d interface names across oFPortCount=%d slots", len(names), oFPortCount)

			seen := make(map[uint]string, len(names))
			collisions := 0

			for _, name := range names {
				port := hashToOFPort(name)
				if prev, exists := seen[port]; exists {
					collisions++
					if *showAllCollisions {
						t.Logf("collision: hashToOFPort(%q) == hashToOFPort(%q) == %d", name, prev, port)
					}
				} else {
					seen[port] = name
				}
			}

			t.Logf("%d hash collisions detected out of %d names (%d unique ofports in range %d–%d)",
				collisions, len(names), len(seen), minOFPort, maxOFPort)
		})
	}
}

// TestResolveOFPort_ManySubfunctions allocates ports for a realistic bridge
// containing physical ports (p0, p1), host PF representors (pf0hpf, pf1hpf),
// VF representors (pf0vfNN), SF representors (en3f0pf0sfNN), and their
// corresponding patch ports (pen3f0pf0sfNNbrsfc, pen3f0pf0sfNNbrhbn).
// Verifies zero collisions across the full set.
func TestResolveOFPort_ManySubfunctions(t *testing.T) {
	used := make(map[uint]bool)
	allocated := make(map[string]uint)

	allocate := func(name string) {
		t.Helper()
		port := resolveOFPort(name, used)
		if port == 0 {
			t.Fatalf("resolveOFPort(%q) returned sentinel 0 with %d ports used", name, len(used))
		}
		if used[port] {
			t.Fatalf("resolveOFPort(%q) = %d collides with a previously allocated port", name, port)
		}
		used[port] = true
		allocated[name] = port
	}

	// Physical ports and host PF representors
	allocate("p0")
	allocate("p1")
	allocate("pf0hpf")
	allocate("pf1hpf")

	// VF representors
	for vf := 0; vf < 16; vf++ {
		allocate(fmt.Sprintf("pf0vf%d", vf))
	}

	// SF representors + patch ports
	for sf := 0; sf < 64; sf++ {
		allocate(fmt.Sprintf("en3f0pf0sf%d", sf))
		allocate(fmt.Sprintf("pen3f0pf0sf%dbrsfc", sf))
		allocate(fmt.Sprintf("pen3f0pf0sf%dbrhbn", sf))
	}

	expectedCount := 4 + 16 + 64*3 // p0,p1,pf0hpf,pf1hpf + 16 VFs + 64*(sf+brsfc+brhbn)
	if len(used) != expectedCount {
		t.Errorf("expected %d unique ports, got %d", expectedCount, len(used))
	}
}

// Ginkgo specs for OVS helpers that need mocked ovsutils.API behavior.
// transactExpect builds an EXPECT().Transact() with ctx + opCount gomock.Any
// matchers, sidestepping the per-arg verbosity of variadic gomock matchers.
//
// Op payloads are matched as gomock.Any. Shape assertions on what the
// production helpers send live on the api.AddPort side where they actually
// have value.
func transactExpect(mockAPI *ovsutils.MockAPI, opCount int) *gomock.Call {
	opMatchers := make([]any, opCount)
	for i := range opMatchers {
		opMatchers[i] = gomock.Any()
	}
	return mockAPI.EXPECT().Transact(gomock.Any(), opMatchers...)
}

// portWithExternalIDs stages an api.Get that populates Port.ExternalIDs,
// so production-code checks on owner see meaningful values.
func portWithExternalIDs(mockAPI *ovsutils.MockAPI, externalIDs map[string]string) *gomock.Call {
	return mockAPI.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, m model.Model) error {
			p, ok := m.(*ovsmodel.Port)
			if !ok {
				Fail(fmt.Sprintf("portWithExternalIDs: unexpected model type %T", m))
				return nil
			}
			p.ExternalIDs = externalIDs
			return nil
		})
}

var _ = Describe("OVS helpers", func() {
	var (
		ctx      context.Context
		mockCtrl *gomock.Controller
		mockAPI  *ovsutils.MockAPI
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockCtrl = gomock.NewController(GinkgoT())
		mockAPI = ovsutils.NewMockAPI(mockCtrl)
	})

	AfterEach(func() { mockCtrl.Finish() })

	Describe("getOvsPortForContIface", func() {
		It("returns the port name when found", func() {
			transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{Rows: []ovsdb.Row{{"name": "p0"}}}}, nil)

			name, found, err := getOvsPortForContIface(ctx, mockAPI, "eth0", "/proc/1/ns/net")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(name).To(Equal("p0"))
		})

		It("returns (\"\", false, nil) when the port is absent", func() {
			transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{}}, nil)

			name, found, err := getOvsPortForContIface(ctx, mockAPI, "eth0", "/proc/1/ns/net")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse())
			Expect(name).To(BeEmpty())
		})

		It("propagates op-level errors", func() {
			transactExpect(mockAPI, 1).
				Return([]ovsdb.OperationResult{{Error: "permission denied", Details: ""}}, nil)

			_, found, err := getOvsPortForContIface(ctx, mockAPI, "eth0", "/proc/1/ns/net")
			Expect(err).To(MatchError(ContainSubstring("permission denied")))
			Expect(found).To(BeFalse())
		})
	})

	Describe("deletePort", func() {
		It("deletes a port when owned by the CNI and on this bridge", func() {
			gomock.InOrder(
				portWithExternalIDs(mockAPI, map[string]string{"owner": ovsPortOwner}),
				mockAPI.EXPECT().IsIfaceInBr(gomock.Any(), "br-test", "p0").Return(true, nil),
				mockAPI.EXPECT().DelPort(gomock.Any(), "br-test", "p0").Return(nil),
			)

			Expect(deletePort(ctx, mockAPI, "br-test", "p0")).To(Succeed())
		})

		It("refuses to delete a port not owned by the CNI", func() {
			portWithExternalIDs(mockAPI, map[string]string{"owner": "someone-else"})

			err := deletePort(ctx, mockAPI, "br-test", "p0")
			Expect(err).To(MatchError(ContainSubstring("not created by ovs-cni")))
		})

		It("errors when the port lives on a different bridge", func() {
			gomock.InOrder(
				portWithExternalIDs(mockAPI, map[string]string{"owner": ovsPortOwner}),
				mockAPI.EXPECT().IsIfaceInBr(gomock.Any(), "br-test", "p0").Return(false, nil),
			)

			err := deletePort(ctx, mockAPI, "br-test", "p0")
			Expect(err).To(MatchError(ContainSubstring("is not on bridge br-test")))
		})

		It("returns error when the port cannot be found", func() {
			mockAPI.EXPECT().Get(gomock.Any(), gomock.Any()).Return(client.ErrNotFound)

			err := deletePort(ctx, mockAPI, "br-test", "missing")
			Expect(err).To(MatchError(ContainSubstring("object not found")))
		})

		It("propagates IsIfaceInBr errors", func() {
			gomock.InOrder(
				portWithExternalIDs(mockAPI, map[string]string{"owner": ovsPortOwner}),
				mockAPI.EXPECT().IsIfaceInBr(gomock.Any(), "br-test", "p0").
					Return(false, errors.New("schema mismatch")),
			)

			err := deletePort(ctx, mockAPI, "br-test", "p0")
			Expect(err).To(MatchError(ContainSubstring("schema mismatch")))
		})

		It("propagates DelPort errors", func() {
			gomock.InOrder(
				portWithExternalIDs(mockAPI, map[string]string{"owner": ovsPortOwner}),
				mockAPI.EXPECT().IsIfaceInBr(gomock.Any(), "br-test", "p0").Return(true, nil),
				mockAPI.EXPECT().DelPort(gomock.Any(), "br-test", "p0").Return(errors.New("constraint")),
			)

			err := deletePort(ctx, mockAPI, "br-test", "p0")
			Expect(err).To(MatchError(ContainSubstring("constraint")))
		})
	})

	Describe("getUsedOFPorts", func() {
		// Prefers ofport over ofport_request and ignores non-positive ofport values
		// ("interface still being configured").
		It("prefers ofport, falls back to ofport_request, and ignores zero/negative values", func() {
			rows := []ovsdb.Row{
				{"ofport": float64(40000), "ofport_request": float64(32800)}, // ofport wins
				{"ofport": float64(-1), "ofport_request": float64(32801)},    // negative ofport -> use request
				{"ofport": float64(40002)},                                   // only ofport
				{"ofport_request": float64(32803)},                           // only request
				{"ofport": float64(0), "ofport_request": float64(0)},         // both invalid, skip
			}
			transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{Rows: rows}}, nil)

			used, err := getUsedOFPorts(ctx, mockAPI)
			Expect(err).NotTo(HaveOccurred())
			Expect(used).To(HaveKey(uint(40000)))
			Expect(used).NotTo(HaveKey(uint(32800)))
			Expect(used).To(HaveKey(uint(32801)))
			Expect(used).To(HaveKey(uint(40002)))
			Expect(used).To(HaveKey(uint(32803)))
			Expect(used).To(HaveLen(4))
		})

		It("returns an empty set when no rows are present", func() {
			transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{}}, nil)

			used, err := getUsedOFPorts(ctx, mockAPI)
			Expect(err).NotTo(HaveOccurred())
			Expect(used).To(BeEmpty())
		})

		It("propagates op-level errors", func() {
			transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{Error: "denied", Details: ""}}, nil)

			_, err := getUsedOFPorts(ctx, mockAPI)
			Expect(err).To(MatchError(ContainSubstring("denied")))
		})
	})

	Describe("createPort", func() {
		iface := &current.Interface{Name: "eth0", Mac: "aa:bb:cc:dd:ee:ff"}

		It("delegates to api.AddPort when ofport_request is explicit", func() {
			mockAPI.EXPECT().AddPort(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, cfg ovsutils.PortConfig) error {
					Expect(cfg.Name).To(Equal("p0"))
					Expect(cfg.BridgeName).To(Equal("br-test"))
					Expect(cfg.OFPortRequest).NotTo(BeNil())
					Expect(*cfg.OFPortRequest).To(Equal(40000))
					Expect(cfg.WaitForOFPortFree).To(BeTrue())
					Expect(cfg.MTU).NotTo(BeNil())
					Expect(*cfg.MTU).To(Equal(1500))
					Expect(cfg.Tag).NotTo(BeNil())
					Expect(*cfg.Tag).To(Equal(100))
					Expect(cfg.Trunks).To(BeNil())
					Expect(cfg.PortExternalIDs).To(HaveKeyWithValue("owner", ovsPortOwner))
					Expect(cfg.PortExternalIDs).To(HaveKeyWithValue("contIface", "eth0"))
					Expect(cfg.InterfaceExternalIDs).To(HaveKeyWithValue("iface-id", "ovn-port"))
					Expect(cfg.InterfaceExternalIDs).To(HaveKeyWithValue(ovsutils.DPFIDKey, "dpf-1"))
					Expect(cfg.InterfaceExternalIDs).To(HaveKeyWithValue("iface-mac", iface.Mac))
					return nil
				})

			err := createPort(ctx, mockAPI, "br-test", "p0", "/proc/1/ns/net", "ovn-port",
				"dpf-1", iface, 40000, 100, nil, "access", 1500, "", "pod-uid")
			Expect(err).NotTo(HaveOccurred())
		})

		It("resolves ofport_request via getUsedOFPorts when caller passes 0", func() {
			// Empty used-set, resolveOFPort returns the hash candidate directly,
			// pin it so the contract catches a regression in the hash mapping.
			wantOFPort := int(hashToOFPort("p0"))
			gomock.InOrder(
				transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{}}, nil),
				mockAPI.EXPECT().AddPort(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, cfg ovsutils.PortConfig) error {
						Expect(cfg.OFPortRequest).NotTo(BeNil())
						Expect(*cfg.OFPortRequest).To(Equal(wantOFPort))
						Expect(cfg.WaitForOFPortFree).To(BeTrue())
						return nil
					}),
			)

			err := createPort(ctx, mockAPI, "br-test", "p0", "/proc/1/ns/net", "ovn-port",
				"dpf-1", iface, 0, 0, nil, "access", 1500, "", "pod-uid")
			Expect(err).NotTo(HaveOccurred())
		})

		It("populates Trunks (not Tag) when portType is trunk", func() {
			mockAPI.EXPECT().AddPort(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, cfg ovsutils.PortConfig) error {
					Expect(cfg.Tag).To(BeNil())
					Expect(cfg.Trunks).To(Equal([]int{10, 20}))
					Expect(cfg.VLANMode).NotTo(BeNil())
					Expect(*cfg.VLANMode).To(Equal(ovsmodel.PortVLANMode("trunk")))
					return nil
				})

			err := createPort(ctx, mockAPI, "br-test", "p0", "/proc/1/ns/net", "ovn-port",
				"dpf-1", iface, 40000, 0, []uint{10, 20}, "trunk", 1500, "", "pod-uid")
			Expect(err).NotTo(HaveOccurred())
		})

		It("propagates getUsedOFPorts errors before reaching AddPort", func() {
			transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{Error: "denied", Details: ""}}, nil)

			err := createPort(ctx, mockAPI, "br-test", "p0", "/proc/1/ns/net", "ovn-port",
				"dpf-1", iface, 0, 0, nil, "access", 1500, "", "pod-uid")
			Expect(err).To(MatchError(ContainSubstring("query used ofports")))
		})

		It("propagates AddPort errors", func() {
			mockAPI.EXPECT().AddPort(gomock.Any(), gomock.Any()).Return(errors.New("duplicate"))

			err := createPort(ctx, mockAPI, "br-test", "p0", "/proc/1/ns/net", "ovn-port",
				"dpf-1", iface, 40000, 0, nil, "access", 1500, "", "pod-uid")
			Expect(err).To(MatchError(ContainSubstring("duplicate")))
		})

		It("omits mtu_request when mtu is below the CRD minimum", func() {
			mockAPI.EXPECT().AddPort(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, cfg ovsutils.PortConfig) error {
					Expect(cfg.MTU).To(BeNil())
					return nil
				})

			err := createPort(ctx, mockAPI, "br-test", "p0", "/proc/1/ns/net", "ovn-port",
				"dpf-1", iface, 40000, 0, nil, "access", 1279, "", "pod-uid")
			Expect(err).NotTo(HaveOccurred())
		})

		It("sets mtu_request when mtu is the CRD minimum", func() {
			mockAPI.EXPECT().AddPort(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, cfg ovsutils.PortConfig) error {
					Expect(cfg.MTU).NotTo(BeNil())
					Expect(*cfg.MTU).To(Equal(1280))
					return nil
				})

			err := createPort(ctx, mockAPI, "br-test", "p0", "/proc/1/ns/net", "ovn-port",
				"dpf-1", iface, 40000, 0, nil, "access", 1280, "", "pod-uid")
			Expect(err).NotTo(HaveOccurred())
		})

		It("omits iface-id/dpf-id/iface-mac when no OVN metadata is supplied", func() {
			mockAPI.EXPECT().AddPort(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, cfg ovsutils.PortConfig) error {
					Expect(cfg.InterfaceExternalIDs).To(BeNil())
					return nil
				})

			err := createPort(ctx, mockAPI, "br-test", "p0", "/proc/1/ns/net", "",
				"", iface, 40000, 0, nil, "access", 1500, "", "pod-uid")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("cleanupStaleHbn", func() {
		It("returns nil when no stale HBN interfaces exist", func() {
			transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{}}, nil)

			Expect(cleanupStaleHbn(ctx, mockAPI, "eth0")).To(Succeed())
		})

		It("deletes each stale interface from br-hbn", func() {
			rows := []ovsdb.Row{
				{"name": "pen3f0pf0sf1brhbn"},
				{"name": "pen3f0pf0sf2brhbn"},
			}
			gomock.InOrder(
				transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{Rows: rows}}, nil),
				mockAPI.EXPECT().DelPort(gomock.Any(), hbnBridge, "pen3f0pf0sf1brhbn").Return(nil),
				mockAPI.EXPECT().DelPort(gomock.Any(), hbnBridge, "pen3f0pf0sf2brhbn").Return(nil),
			)

			Expect(cleanupStaleHbn(ctx, mockAPI, "eth0")).To(Succeed())
		})

		It("returns error when the lookup fails", func() {
			transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{Error: "read denied", Details: ""}}, nil)

			err := cleanupStaleHbn(ctx, mockAPI, "eth0")
			Expect(err).To(MatchError(ContainSubstring("read denied")))
		})

		It("propagates DelPort errors", func() {
			gomock.InOrder(
				transactExpect(mockAPI, 1).
					Return([]ovsdb.OperationResult{{Rows: []ovsdb.Row{{"name": "pen3f0pf0sf1brhbn"}}}}, nil),
				mockAPI.EXPECT().DelPort(gomock.Any(), hbnBridge, "pen3f0pf0sf1brhbn").
					Return(errors.New("port busy")),
			)

			err := cleanupStaleHbn(ctx, mockAPI, "eth0")
			Expect(err).To(MatchError(ContainSubstring("port busy")))
		})
	})

	Describe("cleanupStaleSfc", func() {
		// Same shape as cleanupStaleHbn, just pin the br-sfc + dpf-id wiring.
		It("deletes each stale interface from br-sfc", func() {
			gomock.InOrder(
				transactExpect(mockAPI, 1).
					Return([]ovsdb.OperationResult{{Rows: []ovsdb.Row{{"name": "p0"}}}}, nil),
				mockAPI.EXPECT().DelPort(gomock.Any(), sfcBridge, "p0").Return(nil),
			)

			Expect(cleanupStaleSfc(ctx, mockAPI, "eth0")).To(Succeed())
		})
	})

	Describe("addPatchPort", func() {
		// Each call: DelPort (refresh stale external_ids) + Transact (getUsedOFPorts) + AddPort.

		It("writes SFC-side external_ids for a non-brhbn patch port", func() {
			gomock.InOrder(
				mockAPI.EXPECT().DelPort(gomock.Any(), "br-sfc", "peth0brsfc").Return(nil),
				transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{}}, nil),
				mockAPI.EXPECT().AddPort(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, cfg ovsutils.PortConfig) error {
						Expect(cfg.Name).To(Equal("peth0brsfc"))
						Expect(cfg.BridgeName).To(Equal("br-sfc"))
						Expect(cfg.InterfaceType).To(Equal("patch"))
						Expect(cfg.InterfaceOptions).To(HaveKeyWithValue("peer", "peth0brhbn"))
						Expect(cfg.InterfaceExternalIDs).To(HaveKeyWithValue("iface-id", "eth0"))
						Expect(cfg.InterfaceExternalIDs).To(HaveKeyWithValue(ovsutils.DPFIDKey, "container-iface"))
						Expect(cfg.InterfaceExternalIDs).To(HaveKey("iface-mac"))
						Expect(cfg.PortExternalIDs).To(HaveKeyWithValue("owner", ovsPortOwner))
						return nil
					}),
			)

			err := addPatchPort(ctx, mockAPI, "br-sfc", "peth0brsfc", "peth0brhbn", "eth0", "container-iface")
			Expect(err).NotTo(HaveOccurred())
		})

		It("writes HBN-side external_ids (hbn_rep_ofport/hbn_netdev) for a *brhbn patch port", func() {
			gomock.InOrder(
				mockAPI.EXPECT().DelPort(gomock.Any(), "br-hbn", "peth0brhbn").Return(nil),
				transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{}}, nil),
				mockAPI.EXPECT().AddPort(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, cfg ovsutils.PortConfig) error {
						Expect(cfg.Name).To(Equal("peth0brhbn"))
						Expect(cfg.BridgeName).To(Equal("br-hbn"))
						Expect(cfg.InterfaceType).To(Equal("patch"))
						Expect(cfg.InterfaceOptions).To(HaveKeyWithValue("peer", "peth0brsfc"))
						Expect(cfg.InterfaceExternalIDs).To(HaveKeyWithValue("hbn_rep_ofport", "eth0"))
						Expect(cfg.InterfaceExternalIDs).To(HaveKeyWithValue("hbn_netdev", "container-iface"))
						Expect(cfg.InterfaceExternalIDs).NotTo(HaveKey("iface-id"))
						Expect(cfg.InterfaceExternalIDs).NotTo(HaveKey("iface-mac"))
						return nil
					}),
			)

			err := addPatchPort(ctx, mockAPI, "br-hbn", "peth0brhbn", "peth0brsfc", "eth0", "container-iface")
			Expect(err).NotTo(HaveOccurred())
		})

		It("propagates DelPort errors before querying ofports", func() {
			mockAPI.EXPECT().DelPort(gomock.Any(), "br-sfc", "peth0brsfc").
				Return(errors.New("port busy"))

			err := addPatchPort(ctx, mockAPI, "br-sfc", "peth0brsfc", "peth0brhbn", "eth0", "container-iface")
			Expect(err).To(MatchError(ContainSubstring("delete stale patch port peth0brsfc")))
			Expect(err).To(MatchError(ContainSubstring("port busy")))
		})

		It("propagates getUsedOFPorts errors before reaching AddPort", func() {
			gomock.InOrder(
				mockAPI.EXPECT().DelPort(gomock.Any(), "br-sfc", "peth0brsfc").Return(nil),
				transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{Error: "read denied", Details: ""}}, nil),
			)

			err := addPatchPort(ctx, mockAPI, "br-sfc", "peth0brsfc", "peth0brhbn", "eth0", "container-iface")
			Expect(err).To(MatchError(ContainSubstring("query used ofports")))
		})

		It("propagates AddPort errors", func() {
			gomock.InOrder(
				mockAPI.EXPECT().DelPort(gomock.Any(), "br-sfc", "peth0brsfc").Return(nil),
				transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{}}, nil),
				mockAPI.EXPECT().AddPort(gomock.Any(), gomock.Any()).Return(errors.New("duplicate")),
			)

			err := addPatchPort(ctx, mockAPI, "br-sfc", "peth0brsfc", "peth0brhbn", "eth0", "container-iface")
			Expect(err).To(MatchError(ContainSubstring("duplicate")))
		})

	})

	Describe("createPatch", func() {
		// addPatchPort is exercised in depth above. These tests just pin the
		// orchestration: peer A gets {brA, portA, peer=portB}, peer B gets
		// {brB, portB, peer=portA}, with each invocation issuing the
		// DelPort + Transact + AddPort triple.
		intfName, contIfaceName, brA, brB := "eth0", "container-iface", "br-sfc", "br-hbn"
		portOnBrA := patchPortName(intfName, brA)
		portOnBrB := patchPortName(intfName, brB)

		It("invokes addPatchPort once per bridge with the right peer wiring", func() {
			gomock.InOrder(
				// Peer A on brA, peer = portOnBrB.
				mockAPI.EXPECT().DelPort(gomock.Any(), brA, portOnBrA).Return(nil),
				transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{}}, nil),
				mockAPI.EXPECT().AddPort(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, cfg ovsutils.PortConfig) error {
						Expect(cfg.BridgeName).To(Equal(brA))
						Expect(cfg.Name).To(Equal(portOnBrA))
						Expect(cfg.InterfaceOptions).To(HaveKeyWithValue("peer", portOnBrB))
						return nil
					}),
				// Peer B on brB, peer = portOnBrA.
				mockAPI.EXPECT().DelPort(gomock.Any(), brB, portOnBrB).Return(nil),
				transactExpect(mockAPI, 1).Return([]ovsdb.OperationResult{{}}, nil),
				mockAPI.EXPECT().AddPort(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, cfg ovsutils.PortConfig) error {
						Expect(cfg.BridgeName).To(Equal(brB))
						Expect(cfg.Name).To(Equal(portOnBrB))
						Expect(cfg.InterfaceOptions).To(HaveKeyWithValue("peer", portOnBrA))
						return nil
					}),
			)

			Expect(createPatch(ctx, mockAPI, intfName, contIfaceName, brA, brB)).To(Succeed())
		})

		It("short-circuits when the first peer fails", func() {
			mockAPI.EXPECT().DelPort(gomock.Any(), brA, portOnBrA).
				Return(errors.New("port busy"))
			// No further calls on the second peer.

			err := createPatch(ctx, mockAPI, intfName, contIfaceName, brA, brB)
			Expect(err).To(MatchError(ContainSubstring("port busy")))
		})
	})
})

var _ = Describe("patchPortName", func() {
	It("strips hyphens from the bridge name to form the patch port name", func() {
		Expect(patchPortName("en3f0pf0sf51", "br-sfc")).To(Equal("pen3f0pf0sf51brsfc"))
		Expect(patchPortName("en3f0pf0sf51", "br-hbn")).To(Equal("pen3f0pf0sf51brhbn"))
		Expect(strings.HasSuffix(patchPortName("p0", "br-hbn"), "brhbn")).To(BeTrue())
	})
})
