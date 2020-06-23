package lib

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"testing"

	"git.torproject.org/pluggable-transports/snowflake.git/common/util"
	. "github.com/smartystreets/goconvey/convey"
)

const (
	NATUnknown      = "unknown"
	NATRestricted   = "restricted"
	NATUnrestricted = "unrestricted"
)

type MockTransport struct {
	statusOverride int
	body           []byte
}

// Just returns a response with fake SDP answer.
func (m *MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s := ioutil.NopCloser(bytes.NewReader(m.body))
	r := &http.Response{
		StatusCode: m.statusOverride,
		Body:       s,
	}
	return r, nil
}

type FakeDialer struct{}

func (w FakeDialer) Catch() (*WebRTCPeer, error) {
	fmt.Println("Caught a dummy snowflake.")
	return &WebRTCPeer{}, nil
}

type FakeSocksConn struct {
	net.Conn
	rejected bool
}

func (f FakeSocksConn) Reject() error {
	f.rejected = true
	return nil
}
func (f FakeSocksConn) Grant(addr *net.TCPAddr) error { return nil }

type FakePeers struct{ toRelease *WebRTCPeer }

func (f FakePeers) Collect() (*WebRTCPeer, error) { return &WebRTCPeer{}, nil }
func (f FakePeers) Pop() *WebRTCPeer              { return nil }
func (f FakePeers) Melted() <-chan struct{}       { return nil }

func TestSnowflakeClient(t *testing.T) {

	Convey("Peers", t, func() {
		Convey("Can construct", func() {
			p := NewPeers(1)
			So(p.capacity, ShouldEqual, 1)
			So(p.snowflakeChan, ShouldNotBeNil)
			So(cap(p.snowflakeChan), ShouldEqual, 1)
		})

		Convey("Collecting a Snowflake requires a Tongue.", func() {
			p := NewPeers(1)
			_, err := p.Collect()
			So(err, ShouldNotBeNil)
			So(p.Count(), ShouldEqual, 0)
			// Set the dialer so that collection is possible.
			p.Tongue = FakeDialer{}
			_, err = p.Collect()
			So(err, ShouldBeNil)
			So(p.Count(), ShouldEqual, 1)
			// S
			_, err = p.Collect()
		})

		Convey("Collection continues until capacity.", func() {
			c := 5
			p := NewPeers(c)
			p.Tongue = FakeDialer{}
			// Fill up to capacity.
			for i := 0; i < c; i++ {
				fmt.Println("Adding snowflake ", i)
				_, err := p.Collect()
				So(err, ShouldBeNil)
				So(p.Count(), ShouldEqual, i+1)
			}
			// But adding another gives an error.
			So(p.Count(), ShouldEqual, c)
			_, err := p.Collect()
			So(err, ShouldNotBeNil)
			So(p.Count(), ShouldEqual, c)

			// But popping and closing allows it to continue.
			s := p.Pop()
			s.Close()
			So(s, ShouldNotBeNil)
			So(p.Count(), ShouldEqual, c-1)

			_, err = p.Collect()
			So(err, ShouldBeNil)
			So(p.Count(), ShouldEqual, c)
		})

		Convey("Count correctly purges peers marked for deletion.", func() {
			p := NewPeers(4)
			p.Tongue = FakeDialer{}
			p.Collect()
			p.Collect()
			p.Collect()
			p.Collect()
			So(p.Count(), ShouldEqual, 4)
			s := p.Pop()
			s.Close()
			So(p.Count(), ShouldEqual, 3)
			s = p.Pop()
			s.Close()
			So(p.Count(), ShouldEqual, 2)
		})

		Convey("End Closes all peers.", func() {
			cnt := 5
			p := NewPeers(cnt)
			for i := 0; i < cnt; i++ {
				p.activePeers.PushBack(&WebRTCPeer{})
			}
			So(p.Count(), ShouldEqual, cnt)
			p.End()
			<-p.Melted()
			So(p.Count(), ShouldEqual, 0)
		})

		Convey("Pop skips over closed peers.", func() {
			p := NewPeers(4)
			p.Tongue = FakeDialer{}
			wc1, _ := p.Collect()
			wc2, _ := p.Collect()
			wc3, _ := p.Collect()
			So(wc1, ShouldNotBeNil)
			So(wc2, ShouldNotBeNil)
			So(wc3, ShouldNotBeNil)
			wc1.Close()
			r := p.Pop()
			So(p.Count(), ShouldEqual, 2)
			So(r, ShouldEqual, wc2)
			wc4, _ := p.Collect()
			wc2.Close()
			wc3.Close()
			r = p.Pop()
			So(r, ShouldEqual, wc4)
		})

	})

	Convey("Snowflake", t, func() {

		SkipConvey("Handler Grants correctly", func() {
			socks := &FakeSocksConn{}
			snowflakes := &FakePeers{}

			So(socks.rejected, ShouldEqual, false)
			snowflakes.toRelease = nil
			Handler(socks, snowflakes)
			So(socks.rejected, ShouldEqual, true)
		})
	})

	Convey("Dialers", t, func() {
		Convey("Can construct WebRTCDialer.", func() {
			broker := &BrokerChannel{Host: "test"}
			d := NewWebRTCDialer(broker, nil)
			So(d, ShouldNotBeNil)
			So(d.BrokerChannel, ShouldNotBeNil)
			So(d.BrokerChannel.Host, ShouldEqual, "test")
		})
		SkipConvey("WebRTCDialer can Catch a snowflake.", func() {
			broker := &BrokerChannel{Host: "test"}
			d := NewWebRTCDialer(broker, nil)
			conn, err := d.Catch()
			So(conn, ShouldBeNil)
			So(err, ShouldNotBeNil)
		})
	})

	Convey("Rendezvous", t, func() {
		transport := &MockTransport{
			http.StatusOK,
			[]byte(`{"type":"answer","sdp":"fake"}`),
		}
		fakeOffer, err := util.DeserializeSessionDescription(`{"type":"offer","sdp":"test"}`)
		if err != nil {
			panic(err)
		}

		Convey("Construct BrokerChannel with no front domain", func() {
			b, err := NewBrokerChannel("test.broker", "", transport, false, NATUnknown)
			So(b.url, ShouldNotBeNil)
			So(err, ShouldBeNil)
			So(b.url.Path, ShouldResemble, "test.broker")
			So(b.transport, ShouldNotBeNil)
		})

		Convey("Construct BrokerChannel *with* front domain", func() {
			b, err := NewBrokerChannel("test.broker", "front", transport, false, NATUnknown)
			So(b.url, ShouldNotBeNil)
			So(err, ShouldBeNil)
			So(b.url.Path, ShouldResemble, "test.broker")
			So(b.url.Host, ShouldResemble, "front")
			So(b.transport, ShouldNotBeNil)
		})

		Convey("BrokerChannel.Negotiate responds with answer", func() {
			b, err := NewBrokerChannel("test.broker", "", transport, false, NATUnknown)
			So(err, ShouldBeNil)
			answer, err := b.Negotiate(fakeOffer)
			So(err, ShouldBeNil)
			So(answer, ShouldNotBeNil)
			So(answer.SDP, ShouldResemble, "fake")
		})

		Convey("BrokerChannel.Negotiate fails with 503", func() {
			b, err := NewBrokerChannel("test.broker", "",
				&MockTransport{http.StatusServiceUnavailable, []byte("\n")},
				false, NATUnknown)
			So(err, ShouldBeNil)
			answer, err := b.Negotiate(fakeOffer)
			So(err, ShouldNotBeNil)
			So(answer, ShouldBeNil)
			So(err.Error(), ShouldResemble, BrokerError503)
		})

		Convey("BrokerChannel.Negotiate fails with 400", func() {
			b, err := NewBrokerChannel("test.broker", "",
				&MockTransport{http.StatusBadRequest, []byte("\n")},
				false, NATUnknown)
			So(err, ShouldBeNil)
			answer, err := b.Negotiate(fakeOffer)
			So(err, ShouldNotBeNil)
			So(answer, ShouldBeNil)
			So(err.Error(), ShouldResemble, BrokerError400)
		})

		Convey("BrokerChannel.Negotiate fails with large read", func() {
			b, err := NewBrokerChannel("test.broker", "",
				&MockTransport{http.StatusOK, make([]byte, 100001, 100001)},
				false, NATUnknown)
			So(err, ShouldBeNil)
			answer, err := b.Negotiate(fakeOffer)
			So(err, ShouldNotBeNil)
			So(answer, ShouldBeNil)
			So(err.Error(), ShouldResemble, "unexpected EOF")
		})

		Convey("BrokerChannel.Negotiate fails with unexpected error", func() {
			b, err := NewBrokerChannel("test.broker", "",
				&MockTransport{123, []byte("")}, false, NATUnknown)
			So(err, ShouldBeNil)
			answer, err := b.Negotiate(fakeOffer)
			So(err, ShouldNotBeNil)
			So(answer, ShouldBeNil)
			So(err.Error(), ShouldResemble, BrokerErrorUnexpected)
		})
	})

}
