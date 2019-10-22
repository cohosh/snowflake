package main

import (
	"net"
	"strconv"
	"testing"

	"git.torproject.org/pluggable-transports/snowflake.git/common/proto"
	. "github.com/smartystreets/goconvey/convey"
)

func TestClientAddr(t *testing.T) {
	// good tests
	for _, test := range []struct {
		input    string
		expected net.IP
	}{
		{"1.2.3.4", net.ParseIP("1.2.3.4")},
		{"1:2::3:4", net.ParseIP("1:2::3:4")},
	} {
		useraddr := clientAddr(test.input)
		host, port, err := net.SplitHostPort(useraddr)
		if err != nil {
			t.Errorf("clientAddr(%q) → SplitHostPort error %v", test.input, err)
			continue
		}
		if !test.expected.Equal(net.ParseIP(host)) {
			t.Errorf("clientAddr(%q) → host %q, not %v", test.input, host, test.expected)
		}
		portNo, err := strconv.Atoi(port)
		if err != nil {
			t.Errorf("clientAddr(%q) → port %q", test.input, port)
			continue
		}
		if portNo == 0 {
			t.Errorf("clientAddr(%q) → port %d", test.input, portNo)
		}
	}

	// bad tests
	for _, input := range []string{
		"",
		"abc",
		"1.2.3.4.5",
		"[12::34]",
	} {
		useraddr := clientAddr(input)
		if useraddr != "" {
			t.Errorf("clientAddr(%q) → %q, not %q", input, useraddr, "")
		}
	}
}

func TestFlurries(t *testing.T) {
	Convey("Snowflake protocol", t, func() {

		client, server := net.Pipe()

		c := proto.NewSnowflakeConn()
		s := proto.NewSnowflakeConn()

		Convey("Test flurries", func() {
			flurries := newFlurries()
			pc, _ := net.Pipe() //used as stub OR cons
			c2 := proto.NewSnowflakeConn()

			// Add two new flurries
			go func() {
				c.NewSnowflake(client, true)
				c2.NewSnowflake(client, true)
			}()
			addr, err := proto.ReadSessionID(server)
			flurries.Add(addr, s, pc)
			So(err, ShouldEqual, nil)

			addr, err = proto.ReadSessionID(server)
			flurries.Add(addr, s, pc)
			So(err, ShouldEqual, nil)

			So(len(flurries.flurries), ShouldEqual, 2)

			// Get a flurry
			flurry := flurries.Get(addr)
			So(flurry.Addr, ShouldResemble, addr)
			So(flurry.Conn, ShouldEqual, s)
			So(flurry.Or, ShouldEqual, pc)

			// Delete a flurry
			flurries.Delete(addr)
			So(len(flurries.flurries), ShouldEqual, 1)

		})
	})
}
