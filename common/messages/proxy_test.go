package messages

import (
	"encoding/json"
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestDecodeProxyPollRequest(t *testing.T) {
	Convey("Context", t, func() {
		for _, test := range []struct {
			sid       string
			proxyType string
			version   string
			data      string
			err       error
		}{
			{
				//Version 1.0 proxy message
				"ymbcCMto7KHNGYlp",
				"",
				"",
				`{"Sid":"ymbcCMto7KHNGYlp","Version":"1.0"}`,
				nil,
			},
			{
				//Version 1.1 proxy message
				"ymbcCMto7KHNGYlp",
				"standalone",
				"",
				`{"Sid":"ymbcCMto7KHNGYlp","Version":"1.1","Type":"standalone"}`,
				nil,
			},
			{
				//Version 1.2 proxy message
				"ymbcCMto7KHNGYlp",
				"standalone",
				"1.0",
				`{"Sid":"ymbcCMto7KHNGYlp","Version":"1.1","Type":"standalone", "ProxyVersion":"1.0"}`,
				nil,
			},
			{
				//Version 0.X proxy message:
				"",
				"",
				"",
				"ymbcCMto7KHNGYlp",
				&json.SyntaxError{},
			},
			{
				"",
				"",
				"",
				`{"Sid":"ymbcCMto7KHNGYlp"}`,
				fmt.Errorf(""),
			},
			{
				"",
				"",
				"",
				"{}",
				fmt.Errorf(""),
			},
			{
				"",
				"",
				"",
				`{"Version":"1.0"}`,
				fmt.Errorf(""),
			},
			{
				"",
				"",
				"",
				`{"Version":"2.0"}`,
				fmt.Errorf(""),
			},
		} {
			sid, proxyType, version, err := DecodePollRequest([]byte(test.data))
			So(sid, ShouldResemble, test.sid)
			So(proxyType, ShouldResemble, test.proxyType)
			So(version, ShouldResemble, test.version)
			So(err, ShouldHaveSameTypeAs, test.err)
		}

	})
}

func TestEncodeProxyPollRequests(t *testing.T) {
	Convey("Context", t, func() {
		b, err := EncodePollRequest("ymbcCMto7KHNGYlp", "standalone", "1.0")
		So(err, ShouldEqual, nil)
		sid, proxyType, version, err := DecodePollRequest(b)
		So(sid, ShouldEqual, "ymbcCMto7KHNGYlp")
		So(proxyType, ShouldEqual, "standalone")
		So(version, ShouldEqual, "1.0")
		So(err, ShouldEqual, nil)
	})
}

func TestDecodeProxyPollResponse(t *testing.T) {
	Convey("Context", t, func() {
		for _, test := range []struct {
			offer  string
			update bool
			data   string
			err    error
		}{
			{
				"fake offer",
				false,
				`{"Status":"client match","Offer":"fake offer","Update":false}`,
				nil,
			},
			{
				"",
				false,
				`{"Status":"no match","Update":false}`,
				nil,
			},
			{
				"",
				true,
				`{"Status":"no match","Update":true}`,
				nil,
			},
			{
				"",
				false,
				`{"Status":"no match"}`,
				nil,
			},
			{
				"",
				false,
				`{"Status":"client match","Update":false}`,
				fmt.Errorf("no supplied offer"),
			},
			{
				"",
				false,
				`{"Test":"test","Update":false}`,
				fmt.Errorf(""),
			},
		} {
			offer, update, err := DecodePollResponse([]byte(test.data))
			So(err, ShouldHaveSameTypeAs, test.err)
			So(offer, ShouldResemble, test.offer)
			So(update, ShouldResemble, test.update)
		}

	})
}

func TestEncodeProxyPollResponse(t *testing.T) {
	Convey("Context", t, func() {
		b, err := EncodePollResponse("fake offer", true, false)
		So(err, ShouldEqual, nil)
		offer, update, err := DecodePollResponse(b)
		So(offer, ShouldEqual, "fake offer")
		So(update, ShouldEqual, false)
		So(err, ShouldEqual, nil)

		b, err = EncodePollResponse("", false, true)
		So(err, ShouldEqual, nil)
		offer, update, err = DecodePollResponse(b)
		So(offer, ShouldEqual, "")
		So(update, ShouldEqual, true)
		So(err, ShouldEqual, nil)
	})
}
func TestDecodeProxyAnswerRequest(t *testing.T) {
	Convey("Context", t, func() {
		for _, test := range []struct {
			answer string
			sid    string
			data   string
			err    error
		}{
			{
				"test",
				"test",
				`{"Version":"1.0","Sid":"test","Answer":"test"}`,
				nil,
			},
			{
				"",
				"",
				`{"type":"offer","sdp":"v=0\r\no=- 4358805017720277108 2 IN IP4 [scrubbed]\r\ns=-\r\nt=0 0\r\na=group:BUNDLE data\r\na=msid-semantic: WMS\r\nm=application 56688 DTLS/SCTP 5000\r\nc=IN IP4 [scrubbed]\r\na=candidate:3769337065 1 udp 2122260223 [scrubbed] 56688 typ host generation 0 network-id 1 network-cost 50\r\na=candidate:2921887769 1 tcp 1518280447 [scrubbed] 35441 typ host tcptype passive generation 0 network-id 1 network-cost 50\r\na=ice-ufrag:aMAZ\r\na=ice-pwd:jcHb08Jjgrazp2dzjdrvPPvV\r\na=ice-options:trickle\r\na=fingerprint:sha-256 C8:88:EE:B9:E7:02:2E:21:37:ED:7A:D1:EB:2B:A3:15:A2:3B:5B:1C:3D:D4:D5:1F:06:CF:52:40:03:F8:DD:66\r\na=setup:actpass\r\na=mid:data\r\na=sctpmap:5000 webrtc-datachannel 1024\r\n"}`,
				fmt.Errorf(""),
			},
			{
				"",
				"",
				`{"Version":"1.0","Answer":"test"}`,
				fmt.Errorf(""),
			},
			{
				"",
				"",
				`{"Version":"1.0","Sid":"test"}`,
				fmt.Errorf(""),
			},
		} {
			answer, sid, err := DecodeAnswerRequest([]byte(test.data))
			So(answer, ShouldResemble, test.answer)
			So(sid, ShouldResemble, test.sid)
			So(err, ShouldHaveSameTypeAs, test.err)
		}

	})
}

func TestEncodeProxyAnswerRequest(t *testing.T) {
	Convey("Context", t, func() {
		b, err := EncodeAnswerRequest("test answer", "test sid")
		So(err, ShouldEqual, nil)
		answer, sid, err := DecodeAnswerRequest(b)
		So(answer, ShouldEqual, "test answer")
		So(sid, ShouldEqual, "test sid")
		So(err, ShouldEqual, nil)
	})
}

func TestDecodeProxyAnswerResponse(t *testing.T) {
	Convey("Context", t, func() {
		for _, test := range []struct {
			success bool
			data    string
			err     error
		}{
			{
				true,
				`{"Status":"success"}`,
				nil,
			},
			{
				false,
				`{"Status":"client gone"}`,
				nil,
			},
			{
				false,
				`{"Test":"test"}`,
				fmt.Errorf(""),
			},
		} {
			success, err := DecodeAnswerResponse([]byte(test.data))
			So(success, ShouldResemble, test.success)
			So(err, ShouldHaveSameTypeAs, test.err)
		}

	})
}

func TestEncodeProxyAnswerResponse(t *testing.T) {
	Convey("Context", t, func() {
		b, err := EncodeAnswerResponse(true)
		So(err, ShouldEqual, nil)
		success, err := DecodeAnswerResponse(b)
		So(success, ShouldEqual, true)
		So(err, ShouldEqual, nil)

		b, err = EncodeAnswerResponse(false)
		So(err, ShouldEqual, nil)
		success, err = DecodeAnswerResponse(b)
		So(success, ShouldEqual, false)
		So(err, ShouldEqual, nil)
	})
}
