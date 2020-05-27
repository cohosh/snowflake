package lib

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.torproject.org/pluggable-transports/snowflake.git/common/util"
	"github.com/dchest/uniuri"
	"github.com/pion/sdp/v2"
	"github.com/pion/webrtc/v2"
)

var csvWriter *csv.Writer

func init() {
	f, err := os.OpenFile("proxytest.csv", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}
	csvWriter = csv.NewWriter(f)
}

func Now() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05.000")
}

// Remote WebRTC peer.
// Implements the |Snowflake| interface, which includes
// |io.ReadWriter|, |Resetter|, and |Connector|.
//
// Handles preparation of go-webrtc PeerConnection. Only ever has
// one DataChannel.
type WebRTCPeer struct {
	id        string
	attempt   int
	config    *webrtc.Configuration
	pc        *webrtc.PeerConnection
	transport SnowflakeDataChannel // Holds the WebRTC DataChannel.
	broker    *BrokerChannel

	offerChannel  chan *webrtc.SessionDescription
	answerChannel chan *webrtc.SessionDescription
	errorChannel  chan error
	recvPipe      *io.PipeReader
	writePipe     *io.PipeWriter
	lastReceive   time.Time
	buffer        bytes.Buffer
	reset         chan struct{}

	closed bool

	lock sync.Mutex // Synchronization for DataChannel destruction
	once sync.Once  // Synchronization for PeerConnection destruction

	hasRead, hasWritten bool

	BytesLogger
}

func (c *WebRTCPeer) Out(varname, value string) {
	csvWriter.Write([]string{c.id, strconv.Itoa(c.attempt), varname, value})
	csvWriter.Flush()
	err := csvWriter.Error()
	if err != nil {
		panic(err)
	}
}

// Construct a WebRTC PeerConnection.
func NewWebRTCPeer(config *webrtc.Configuration,
	broker *BrokerChannel) *WebRTCPeer {
	connection := new(WebRTCPeer)
	connection.id = "snowflake-" + uniuri.New()
	connection.config = config
	connection.broker = broker
	connection.offerChannel = make(chan *webrtc.SessionDescription, 1)
	connection.answerChannel = make(chan *webrtc.SessionDescription, 1)
	// Error channel is mostly for reporting during the initial SDP offer
	// creation & local description setting, which happens asynchronously.
	connection.errorChannel = make(chan error, 1)
	connection.reset = make(chan struct{}, 1)

	// Override with something that's not NullLogger to have real logging.
	connection.BytesLogger = &BytesNullLogger{}

	// Pipes remain the same even when DataChannel gets switched.
	connection.recvPipe, connection.writePipe = io.Pipe()
	return connection
}

// Read bytes from local SOCKS.
// As part of |io.ReadWriter|
func (c *WebRTCPeer) Read(b []byte) (int, error) {
	return c.recvPipe.Read(b)
}

// Writes bytes out to remote WebRTC.
// As part of |io.ReadWriter|
func (c *WebRTCPeer) Write(b []byte) (int, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.BytesLogger.AddOutbound(len(b))
	// TODO: Buffering could be improved / separated out of WebRTCPeer.
	if nil == c.transport {
		log.Printf("Buffered %d bytes --> WebRTC", len(b))
		c.buffer.Write(b)
	} else {
		if !c.hasWritten {
			c.Out("ts_first_send", Now())
		}
		c.hasWritten = true
		c.transport.Send(b)
	}
	return len(b), nil
}

// As part of |Snowflake|
func (c *WebRTCPeer) Close() error {
	c.once.Do(func() {
		c.closed = true
		c.cleanup()
		c.Reset()
		log.Printf("WebRTC: Closing")
	})
	return nil
}

// As part of |Resetter|
func (c *WebRTCPeer) Reset() {
	if nil == c.reset {
		return
	}
	c.reset <- struct{}{}
}

// As part of |Resetter|
func (c *WebRTCPeer) WaitForReset() { <-c.reset }

// Prevent long-lived broken remotes.
// Should also update the DataChannel in underlying go-webrtc's to make Closes
// more immediate / responsive.
func (c *WebRTCPeer) checkForStaleness() {
	c.lastReceive = time.Now()
	for {
		if c.closed {
			return
		}
		if time.Since(c.lastReceive) > SnowflakeTimeout {
			c.Out("ts_idle_timeout", Now())
			log.Printf("WebRTC: No messages received for %v -- closing stale connection.",
				SnowflakeTimeout)
			c.Close()
			return
		}
		<-time.After(time.Second)
	}
}

// As part of |Connector| interface.
func (c *WebRTCPeer) Connect() error {
	log.Println(c.id, " connecting...")
	// TODO: When go-webrtc is more stable, it's possible that a new
	// PeerConnection won't need to be re-prepared each time.
	err := c.preparePeerConnection()
	if err != nil {
		return err
	}
	err = c.establishDataChannel()
	if err != nil {
		// nolint: golint
		return errors.New("WebRTC: Could not establish DataChannel")
	}
	err = c.exchangeSDP()
	if err != nil {
		return err
	}
	go c.checkForStaleness()
	return nil
}

// Create and prepare callbacks on a new WebRTC PeerConnection.
func (c *WebRTCPeer) preparePeerConnection() error {
	if nil != c.pc {
		if err := c.pc.Close(); err != nil {
			log.Printf("c.pc.Close returned error: %v", err)
		}
		c.pc = nil
	}

	s := webrtc.SettingEngine{}
	s.SetTrickle(true)
	api := webrtc.NewAPI(webrtc.WithSettingEngine(s))
	pc, err := api.NewPeerConnection(*c.config)
	if err != nil {
		log.Printf("NewPeerConnection ERROR: %s", err)
		return err
	}
	// Prepare PeerConnection callbacks.
	// Allow candidates to accumulate until ICEGatheringStateComplete.
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			log.Printf("WebRTC: Done gathering candidates")
		} else {
			log.Printf("WebRTC: Got ICE candidate: %s", candidate.String())
		}
	})
	pc.OnICEGatheringStateChange(func(state webrtc.ICEGathererState) {
		if state == webrtc.ICEGathererStateComplete {
			log.Println("WebRTC: ICEGatheringStateComplete")
			c.offerChannel <- pc.LocalDescription()
		}
	})
	// This callback is not expected, as the Client initiates the creation
	// of the data channel, not the remote peer.
	pc.OnDataChannel(func(channel *webrtc.DataChannel) {
		log.Println("OnDataChannel")
		panic("Unexpected OnDataChannel!")
	})
	c.pc = pc
	go func() {
		offer, err := pc.CreateOffer(nil)
		// TODO: Potentially timeout and retry if ICE isn't working.
		if err != nil {
			c.errorChannel <- err
			return
		}
		log.Println("WebRTC: Created offer")
		err = pc.SetLocalDescription(offer)
		if err != nil {
			c.errorChannel <- err
			return
		}
		log.Println("WebRTC: Set local description")
	}()
	log.Println("WebRTC: PeerConnection created.")
	return nil
}

// Create a WebRTC DataChannel locally.
func (c *WebRTCPeer) establishDataChannel() error {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.transport != nil {
		panic("Unexpected datachannel already exists!")
	}
	ordered := true
	dataChannelOptions := &webrtc.DataChannelInit{
		Ordered: &ordered,
	}
	dc, err := c.pc.CreateDataChannel(c.id, dataChannelOptions)
	// Triggers "OnNegotiationNeeded" on the PeerConnection, which will prepare
	// an SDP offer while other goroutines operating on this struct handle the
	// signaling. Eventually fires "OnOpen".
	if err != nil {
		log.Printf("CreateDataChannel ERROR: %s", err)
		return err
	}
	dc.OnOpen(func() {
		c.lock.Lock()
		defer c.lock.Unlock()
		log.Println("WebRTC: DataChannel.OnOpen")
		c.Out("ts_open", Now())
		if nil != c.transport {
			panic("WebRTC: transport already exists.")
		}
		// Flush buffered outgoing SOCKS data if necessary.
		if c.buffer.Len() > 0 {
			if !c.hasWritten {
				c.Out("ts_first_send", Now())
			}
			c.hasWritten = true
			dc.Send(c.buffer.Bytes())
			log.Println("Flushed", c.buffer.Len(), "bytes.")
			c.buffer.Reset()
		}
		// Then enable the datachannel.
		c.transport = dc
	})
	dc.OnClose(func() {
		c.lock.Lock()
		// Future writes will go to the buffer until a new DataChannel is available.
		if nil == c.transport {
			// Closed locally, as part of a reset.
			log.Println("WebRTC: DataChannel.OnClose [locally]")
			c.lock.Unlock()
			return
		}
		// Closed remotely, need to reset everything.
		// Disable the DataChannel as a write destination.
		log.Println("WebRTC: DataChannel.OnClose [remotely]")
		c.transport = nil
		dc.Close()
		// Unlock before Close'ing, since it calls cleanup and asks for the
		// lock to check if the transport needs to be be deleted.
		c.lock.Unlock()
		c.Close()
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if len(msg.Data) <= 0 {
			log.Println("0 length message---")
		}
		if !c.hasRead {
			c.Out("ts_first_recv", Now())
		}
		c.hasRead = true
		c.BytesLogger.AddInbound(len(msg.Data))
		n, err := c.writePipe.Write(msg.Data)
		if err != nil {
			// TODO: Maybe shouldn't actually close.
			log.Println("Error writing to SOCKS pipe")
			if inerr := c.writePipe.CloseWithError(err); inerr != nil {
				log.Printf("c.writePipe.CloseWithError returned error: %v", inerr)
			}
		}
		if n != len(msg.Data) {
			log.Println("Error: short write")
			panic("short write")
		}
		c.lastReceive = time.Now()
	})
	log.Println("WebRTC: DataChannel created.")
	return nil
}

func (c *WebRTCPeer) sendOfferToBroker() {
	if nil == c.broker {
		return
	}
	offer := c.pc.LocalDescription()
	offerSDP := util.StripLocalAddresses(offer.SDP)
	c.Out("offer_sdp", offerSDP)
	summarizeSDP(c, "offer", offerSDP)
	c.Out("ts_broker_start", Now())
	answer, err := c.broker.Negotiate(offer)
	c.Out("ts_broker_end", Now())
	if nil != err || nil == answer {
		c.Out("broker_err", err.Error())
		log.Printf("BrokerChannel Error: %s", err)
		answer = nil
	}
	c.answerChannel <- answer
}

func summarizeSDP(c *WebRTCPeer, label, text string) {
	var desc sdp.SessionDescription
	err := desc.Unmarshal([]byte(text))
	if err != nil {
		panic(err)
	}
	numTransportTCP := 0
	numTransportUDP := 0
	numTransportOther := 0
	numAddressIPv4 := 0
	numAddressIPv6 := 0
	numAddressLocalHostname := 0
	numAddressOther := 0
	for _, m := range desc.MediaDescriptions {
		if ci := m.ConnectionInformation; ci != nil {
			c.Out(label+"_c_network_type", ci.NetworkType)
			c.Out(label+"_c_address_type", ci.AddressType)
			if ip := net.ParseIP(ci.Address.Address); ip != nil {
				if ip.IsUnspecified() {
					c.Out(label+"_c_address_zero", "T")
				} else {
					c.Out(label+"_c_address_zero", "F")
				}
			}
		}
		i := 0
		for _, a := range m.Attributes {
			candidate, err := a.ToICECandidate()
			if err != nil {
				continue
			}
			c.Out(fmt.Sprintf(label+"_candidate_transport.%d", i), candidate.Protocol)
			c.Out(fmt.Sprintf(label+"_candidate_priority.%d", i), strconv.FormatUint(uint64(candidate.Priority), 10))
			c.Out(fmt.Sprintf(label+"_candidate_typ.%d", i), candidate.Typ)
			switch strings.ToLower(candidate.Protocol) {
			case "tcp":
				numTransportTCP += 1
			case "udp":
				numTransportUDP += 1
			default:
				numTransportOther += 1
			}
			if ip := net.ParseIP(candidate.Address); ip != nil {
				if ip.To4() != nil {
					numAddressIPv4 += 1
					c.Out(fmt.Sprintf(label+"_candidate_address_type.%d", i), "IP4")
				} else {
					numAddressIPv6 += 1
					c.Out(fmt.Sprintf(label+"_candidate_address_type.%d", i), "IP6")
				}
			} else if strings.HasSuffix(strings.ToLower(candidate.Address), ".local") {
				c.Out(fmt.Sprintf(label+"_candidate_address_type.%d", i), "local_hostname")
				numAddressLocalHostname += 1
			} else {
				c.Out(fmt.Sprintf(label+"_candidate_address_type.%d", i), "other")
				numAddressOther += 1
			}
			i++
		}
	}
	c.Out(label+"_num_transport_tcp", strconv.Itoa(numTransportTCP))
	c.Out(label+"_num_transport_udp", strconv.Itoa(numTransportUDP))
	c.Out(label+"_num_transport_other", strconv.Itoa(numTransportOther))
	c.Out(label+"_num_address_ipv4", strconv.Itoa(numAddressIPv4))
	c.Out(label+"_num_address_ipv6", strconv.Itoa(numAddressIPv6))
	c.Out(label+"_num_address_local_hostname", strconv.Itoa(numAddressLocalHostname))
	c.Out(label+"_num_address_other", strconv.Itoa(numAddressOther))
}

// Block until an SDP offer is available, send it to either
// the Broker or signal pipe, then await for the SDP answer.
func (c *WebRTCPeer) exchangeSDP() error {
	select {
	case <-c.offerChannel:
	case err := <-c.errorChannel:
		log.Println("Failed to prepare offer", err)
		c.Close()
		return err
	}
	// Keep trying the same offer until a valid answer arrives.
	var ok bool
	var answer *webrtc.SessionDescription
	for nil == answer {
		go c.sendOfferToBroker()
		answer, ok = <-c.answerChannel // Blocks...
		if !ok || nil == answer {
			log.Printf("Failed to retrieve answer. Retrying in %v", ReconnectTimeout)
			<-time.After(ReconnectTimeout)
			answer = nil
		}
		if answer == nil {
			c.attempt += 1
		}
	}
	log.Printf("Received Answer.\n")

	c.Out("answer_sdp", answer.SDP)
	summarizeSDP(c, "answer", answer.SDP)

	err := c.pc.SetRemoteDescription(*answer)
	if nil != err {
		log.Println("WebRTC: Unable to SetRemoteDescription:", err)
		return err
	}
	return nil
}

// Close all channels and transports
func (c *WebRTCPeer) cleanup() {
	if nil != c.offerChannel {
		close(c.offerChannel)
	}
	if nil != c.answerChannel {
		close(c.answerChannel)
	}
	if nil != c.errorChannel {
		close(c.errorChannel)
	}
	// Close this side of the SOCKS pipe.
	if nil != c.writePipe {
		c.writePipe.Close()
		c.writePipe = nil
	}
	c.lock.Lock()
	if nil != c.transport {
		log.Printf("WebRTC: closing DataChannel")
		dataChannel := c.transport
		// Setting transport to nil *before* dc Close indicates to OnClose that
		// this was locally triggered.
		c.transport = nil
		// Release the lock before calling DeleteDataChannel (which in turn
		// calls Close on the dataChannel), but after nil'ing out the transport,
		// since otherwise we'll end up in the onClose handler in a deadlock.
		c.lock.Unlock()
		if c.pc == nil {
			panic("DataChannel w/o PeerConnection, not good.")
		}
		dataChannel.(*webrtc.DataChannel).Close()
	} else {
		c.lock.Unlock()
	}
	if nil != c.pc {
		log.Printf("WebRTC: closing PeerConnection")
		err := c.pc.Close()
		if nil != err {
			log.Printf("Error closing peerconnection...")
		}
		c.pc = nil
	}
}
