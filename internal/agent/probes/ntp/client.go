package ntp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	packetSize  = 48
	defaultPort = "123"

	// ntpEpochOffset is the number of seconds between the NTP epoch
	// (1900-01-01) and the Unix epoch (1970-01-01).
	ntpEpochOffset = 2208988800
	// era1Offset maps an NTP era-1 timestamp onto Unix seconds. NTP encodes
	// seconds in 32 bits, which wraps in 2036; timestamps with the high bit
	// clear belong to the next era. Handling it is not futureproofing for its
	// own sake — a server whose own clock is wrong can hand us an era-1
	// timestamp today, and reading it as era 0 yields an offset off by 136
	// years rather than a rejected sample.
	era1Offset = 4294967296 - ntpEpochOffset

	versionNTPv4 = 4
	modeClient   = 3
	modeServer   = 4

	leapUnsynchronised = 3
	maxStratum         = 15
)

// errUnsynchronised is returned for a server that answers but declares its own
// clock unsteered. Its timestamps are worthless as a reference, and treating
// them as one would report a fabricated offset for our host.
var errUnsynchronised = errors.New("the server reports its own clock as unsynchronised")

// errRefused is returned for a kiss-o'-death response. It is a distinct
// condition from unreachable: the server is up and is telling us to stop.
var errRefused = errors.New("the server refused the request")

// errUnreachable wraps every failure to complete the exchange at the network
// level. It is kept apart from a malformed or refused answer because the two
// call for opposite actions: nothing arrived usually means outbound UDP 123 is
// filtered, which is a firewall to open, not a time source to fix.
var errUnreachable = errors.New("no answer from the server")

// sample is one completed NTP exchange.
//
// offset is the correction that would have to be applied to the local clock to
// agree with the server: positive means the local clock is behind. roundTrip is
// the measured time the exchange spent in flight, and it is the confidence
// attached to offset — see best().
type sample struct {
	offset         time.Duration
	roundTrip      time.Duration
	stratum        uint8
	leap           uint8
	rootDelay      time.Duration
	rootDispersion time.Duration
}

// query performs one NTP exchange with server and returns the resulting sample.
// server may carry an explicit port; otherwise 123 is used.
func query(server string, timeout time.Duration) (sample, error) {
	addr := server
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, defaultPort)
	}

	conn, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		return sample{}, fmt.Errorf("%w: connecting to %s: %v", errUnreachable, addr, err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return sample{}, fmt.Errorf("setting deadline for %s: %w", addr, err)
	}

	req := make([]byte, packetSize)
	req[0] = versionNTPv4<<3 | modeClient

	// t1 is read as late as possible before the write and t4 as early as
	// possible after the read, so the interval we measure contains as little
	// of our own processing as we can manage.
	t1 := time.Now()
	binary.BigEndian.PutUint64(req[40:48], toNTP(t1))

	if _, err := conn.Write(req); err != nil {
		return sample{}, fmt.Errorf("%w: sending to %s: %v", errUnreachable, addr, err)
	}

	resp := make([]byte, packetSize)
	n, err := conn.Read(resp)
	t4 := time.Now()
	if err != nil {
		return sample{}, fmt.Errorf("%w: waiting for %s: %v", errUnreachable, addr, err)
	}
	if n < packetSize {
		return sample{}, fmt.Errorf("%s answered %d bytes, want at least %d", addr, n, packetSize)
	}

	return decode(resp, t1, t4)
}

// decode validates a server response and computes the clock offset from the
// four timestamps of the NTP exchange:
//
//	t1  we sent            t4  we received
//	t2  the server received    t3  the server sent
//
//	offset = ((t2 - t1) + (t3 - t4)) / 2
//	delay  =  (t4 - t1) - (t3 - t2)
//
// The offset formula assumes the request and the reply spent the same time in
// flight. That assumption is the accuracy limit of the whole method and it
// cannot be checked from inside a single exchange — see best().
func decode(resp []byte, t1, t4 time.Time) (sample, error) {
	leap := resp[0] >> 6
	mode := resp[0] & 0x07
	stratum := resp[1]

	if mode != modeServer {
		return sample{}, fmt.Errorf("response carries mode %d, want %d (server)", mode, modeServer)
	}

	// A server echoes our transmit timestamp verbatim in the originate field.
	// A packet carrying anything else is a datagram from an earlier cycle or an
	// off-path forgery; either way its timestamps must not become a reading.
	if origin := binary.BigEndian.Uint64(resp[24:32]); origin != toNTP(t1) {
		return sample{}, errors.New("response does not echo our request timestamp: stale or forged")
	}

	if stratum == 0 {
		return sample{}, kissOfDeath(resp[12:16])
	}
	if stratum > maxStratum {
		return sample{}, fmt.Errorf("stratum %d is outside the valid range 1..%d", stratum, maxStratum)
	}
	if leap == leapUnsynchronised {
		return sample{}, errUnsynchronised
	}

	t2 := fromNTP(binary.BigEndian.Uint64(resp[32:40]))
	t3 := fromNTP(binary.BigEndian.Uint64(resp[40:48]))
	if t2.IsZero() || t3.IsZero() {
		return sample{}, errors.New("response carries a zero timestamp")
	}

	// t2 and t3 come off the wire and carry no monotonic reading, so these two
	// subtractions compare wall clocks — which is the point, that difference IS
	// the offset. t4.Sub(t1) does have a monotonic reading on both sides, so the
	// round trip stays correct even if something steps the clock mid-exchange.
	offset := (t2.Sub(t1) + t3.Sub(t4)) / 2
	roundTrip := t4.Sub(t1) - t3.Sub(t2)
	if roundTrip < 0 {
		roundTrip = 0
	}

	return sample{
		offset:         offset,
		roundTrip:      roundTrip,
		stratum:        stratum,
		leap:           leap,
		rootDelay:      shortToDuration(binary.BigEndian.Uint32(resp[4:8])),
		rootDispersion: shortToDuration(binary.BigEndian.Uint32(resp[8:12])),
	}, nil
}

// kissOfDeath decodes the four-character code a stratum-0 response carries in
// the reference-identifier field. RATE means we are polling faster than the
// server allows, DENY and RSTR mean it refuses us outright.
//
// Such a packet carries no usable timestamps, so there is nothing to salvage
// from it — and reading one as a measurement is precisely the behaviour the
// code exists to stop.
func kissOfDeath(refID []byte) error {
	code := strings.TrimRight(string(refID), "\x00 ")
	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return fmt.Errorf("%w (stratum 0, unreadable reason code)", errRefused)
		}
	}
	if code == "" {
		return fmt.Errorf("%w (stratum 0, no reason code)", errRefused)
	}
	return fmt.Errorf("%w: %s", errRefused, code)
}

// best returns the sample with the smallest round trip.
//
// This is the entire accuracy story of a client that gets a handful of packets
// per cycle. The offset formula splits the round trip evenly between the two
// directions, so its error is half of however asymmetric the path actually was.
// Queuing is what makes a path asymmetric, and the exchange that queued least
// is the one that travelled closest to symmetric.
//
// It reduces the error. It cannot measure it — which is why roundTrip is
// published alongside the offset rather than discarded: a large or jumping
// round trip is the reader's signal that the offset next to it is coarse.
func best(samples []sample) sample {
	winner := samples[0]
	for _, s := range samples[1:] {
		if s.roundTrip < winner.roundTrip {
			winner = s
		}
	}
	return winner
}

// toNTP converts an instant to the 64-bit NTP timestamp format: 32 bits of
// seconds since 1900 followed by 32 bits of fractional second.
func toNTP(t time.Time) uint64 {
	sec := uint64(t.Unix() + ntpEpochOffset)
	frac := uint64(t.Nanosecond()) << 32 / uint64(time.Second)
	return sec<<32 | frac
}

// fromNTP converts a 64-bit NTP timestamp to an instant. A zero value means
// "unset" in the protocol and maps to the zero time rather than to 1900.
func fromNTP(v uint64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	sec := int64(v >> 32)
	nsec := int64((v & 0xFFFFFFFF) * uint64(time.Second) >> 32)
	if sec&0x80000000 != 0 {
		return time.Unix(sec-ntpEpochOffset, nsec)
	}
	return time.Unix(sec+era1Offset, nsec)
}

// shortToDuration converts the NTP short format (16 bits of seconds, 16 bits of
// fraction) used by the root delay and root dispersion fields.
func shortToDuration(v uint32) time.Duration {
	return time.Duration(float64(v) / 65536.0 * float64(time.Second))
}
