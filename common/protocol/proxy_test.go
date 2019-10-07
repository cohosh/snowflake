package proto

import (
	"encoding/json"
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestDecodeProxyPollRequest(t *testing.T) {
	Convey("Context", t, func() {
		for _, test := range []struct {
			sid  string
			data string
			err  error
		}{
			{
				//Version 1.0 proxy message
				"ymbcCMto7KHNGYlp",
				"{\"Sid\":\"ymbcCMto7KHNGYlp\",\"Version\":\"1.0\"}",
				nil,
			},
			{
				//Version 0.X proxy message:
				"",
				"ymbcCMto7KHNGYlp",
				&json.SyntaxError{},
			},
			{
				"",
				"{\"Sid\":\"ymbcCMto7KHNGYlp\"}",
				fmt.Errorf(""),
			},
			{
				"",
				"{}",
				fmt.Errorf(""),
			},
			{
				"",
				"{\"Version\":\"1.0\"}",
				fmt.Errorf(""),
			},
			{
				"",
				"{\"Version\":\"2.0\"}",
				fmt.Errorf(""),
			},
		} {
			sid, err := DecodePollRequest([]byte(test.data))
			So(sid, ShouldResemble, test.sid)
			So(err, ShouldHaveSameTypeAs, test.err)
		}

	})
}

func TestEncodeProxyPollRequests(t *testing.T) {
	Convey("Context", t, func() {
		b, err := EncodePollRequest("ymbcCMto7KHNGYlp")
		So(err, ShouldEqual, nil)
		sid, err := DecodePollRequest(b)
		So(sid, ShouldEqual, "ymbcCMto7KHNGYlp")
		So(err, ShouldEqual, nil)
	})
}

func TestDecodeProxyPollResponse(t *testing.T) {
	Convey("Context", t, func() {
		for _, test := range []struct {
			offer string
			data  string
			err   error
		}{
			{
				"test",
				"{\"Status\":\"client match\",\"Offer\":\"test\"}",
				nil,
			},
			{
				"",
				"{\"Status\":\"no match\",\"Offer\":\"test\"}",
				nil,
			},
			{
				"",
				"{\"Status\":\"client match\"}",
				fmt.Errorf(""),
			},
			{
				"",
				"ymbcCMto7KHNGYlp",
				&json.SyntaxError{},
			},
		} {
			offer, err := DecodePollResponse([]byte(test.data))
			So(offer, ShouldResemble, test.offer)
			So(err, ShouldHaveSameTypeAs, test.err)
		}

	})
}

func TestEncodeProxyPollResponse(t *testing.T) {
	Convey("Context", t, func() {
		b, err := EncodePollResponse("test offer", true)
		So(err, ShouldEqual, nil)
		offer, err := DecodePollResponse(b)
		So(offer, ShouldEqual, "test offer")
		So(err, ShouldEqual, nil)
	})
}
