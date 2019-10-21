package lib

import (
	"errors"
	"log"
	"net"

	"git.torproject.org/pluggable-transports/snowflake.git/common/proto"
)

const (
	ReconnectTimeout = 10
	SnowflakeTimeout = 30
)

// When a connection handler starts, +1 is written to this channel; when it
// ends, -1 is written.
var HandlerChan = make(chan int)

// Given an accepted SOCKS connection, establish a WebRTC connection to the
// remote peer and exchange traffic.
func Handler(socks SocksConnector, snowflakes SnowflakeCollector) error {
	HandlerChan <- 1
	defer func() {
		HandlerChan <- -1
	}()
	defer socks.Close()
	sConn := proto.NewSnowflakeConn()

	// Continuously read from SOCKS connection
	go func() {
		_, err := proto.Proxy(sConn, socks)
		log.Printf("Closed SOCKS connection with error: %s", err.Error())
		socks.Close()
		sConn.Close()
	}()

	for {
		// Obtain an available WebRTC remote. May block.
		snowflake := snowflakes.Pop()
		if nil == snowflake {
			return errors.New("handler: Received invalid Snowflake")
		}
		log.Println("---- Handler: snowflake assigned ----")
		err := socks.Grant(&net.TCPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			return err
		}

		go func() {
			// When WebRTC resets, log message
			snowflake.WaitForReset()
			log.Println("Snowflake reset, gathering new snowflake")
		}()

		sConn.NewSnowflake(snowflake, true)

		// Begin exchanging data. Either WebRTC or localhost SOCKS will close first.
		// In eithercase, this closes the handler and induces a new handler.
		log.Println("---- Copy Loop started, snowflake opened ---")
		rclose, err := proto.Proxy(socks, sConn)
		log.Println("---- Copy Loop ended, snowflake closed ---")
		if !rclose {
			log.Printf("error writing to SOCKS connection: %s", err.Error())
			socks.Close()
			sConn.Close()
			break
		}
		snowflake.Close()
	}
	log.Println("---- Handler: closed ---")
	return nil
}
