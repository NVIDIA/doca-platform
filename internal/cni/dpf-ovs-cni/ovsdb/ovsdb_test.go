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

package ovsdb

import (
	"flag"
	"fmt"
	"testing"
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

// ---------- ofportWaitOp tests ----------

// TestOfportWaitOp_Structure verifies that ofportWaitOp produces a valid
// OVSDB "wait" operation with the correct fields for collision detection.
func TestOfportWaitOp_Structure(t *testing.T) {
	op := ofportWaitOp(40000)

	if op.Op != "wait" {
		t.Errorf("Op = %q, want %q", op.Op, "wait")
	}
	if op.Table != "Interface" {
		t.Errorf("Table = %q, want %q", op.Table, "Interface")
	}
	if op.Until != "!=" {
		t.Errorf("Until = %q, want %q", op.Until, "!=")
	}
	if op.Timeout == nil || *op.Timeout != 0 {
		t.Errorf("Timeout = %v, want pointer to 0", op.Timeout)
	}
	if len(op.Columns) != 1 || op.Columns[0] != "ofport_request" {
		t.Errorf("Columns = %v, want [ofport_request]", op.Columns)
	}
	if len(op.Where) != 1 {
		t.Fatalf("Where has %d conditions, want 1", len(op.Where))
	}
	if len(op.Rows) != 1 {
		t.Fatalf("Rows has %d entries, want 1", len(op.Rows))
	}
	if v, ok := op.Rows[0]["ofport_request"]; !ok || v != uint(40000) {
		t.Errorf("Rows[0][ofport_request] = %v, want 40000", v)
	}
}

// TestOfportWaitOp_DifferentPorts verifies that ofportWaitOp produces
// distinct operations for different port numbers.
func TestOfportWaitOp_DifferentPorts(t *testing.T) {
	op1 := ofportWaitOp(minOFPort)
	op2 := ofportWaitOp(maxOFPort)

	v1 := op1.Rows[0]["ofport_request"]
	v2 := op2.Rows[0]["ofport_request"]
	if v1 == v2 {
		t.Errorf("ofportWaitOp should produce different Rows for different ports")
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
// createInterfaceOperation treats 0 as "omit ofport_request" and lets OVS
// auto-assign a port number.
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
// real CreatePort/createPeer behavior. Verifies no two interfaces collide.
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
