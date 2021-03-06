//Package for communication with the snowflake broker

//import "git.torproject.org/pluggable-transports/snowflake.git/common/messages"
package messages

import (
	"encoding/json"
	"fmt"
	"strings"
)

const version = "1.2"

/* Version 1.2 specification:

== ProxyPollRequest ==
{
  Sid: [generated session id of proxy],
  Version: 1.2,
  Type: ["badge"|"webext"|"standalone"]
  NAT: ["unknown"|"restricted"|"unrestricted"]
}

== ProxyPollResponse ==
1) If a client is matched:
HTTP 200 OK
{
  Status: "client match",
  {
    type: offer,
    sdp: [WebRTC SDP]
  },
  NAT: ["unknown"|"restricted"|"unrestricted"]
}

2) If a client is not matched:
HTTP 200 OK

{
    Status: "no match"
}

3) If the request is malformed:
HTTP 400 BadRequest

== ProxyAnswerRequest ==
{
  Sid: [generated session id of proxy],
  Version: 1.2,
  Answer:
  {
    type: answer,
    sdp: [WebRTC SDP]
  }
}

== ProxyAnswerResponse ==
1) If the client retrieved the answer:
HTTP 200 OK

{
  Status: "success"
}

2) If the client left:
HTTP 200 OK

{
  Status: "client gone"
}

3) If the request is malformed:
HTTP 400 BadRequest

*/

type ProxyPollRequest struct {
	Sid     string
	Version string
	Type    string
	NAT     string
}

func EncodePollRequest(sid string, proxyType string, natType string) ([]byte, error) {
	return json.Marshal(ProxyPollRequest{
		Sid:     sid,
		Version: version,
		Type:    proxyType,
		NAT:     natType,
	})
}

// Decodes a poll message from a snowflake proxy and returns the
// sid and proxy type of the proxy on success and an error if it failed
func DecodePollRequest(data []byte) (string, string, string, error) {
	var message ProxyPollRequest

	err := json.Unmarshal(data, &message)
	if err != nil {
		return "", "", "", err
	}

	majorVersion := strings.Split(message.Version, ".")[0]
	if majorVersion != "1" {
		return "", "", "", fmt.Errorf("using unknown version")
	}

	// Version 1.x requires an Sid
	if message.Sid == "" {
		return "", "", "", fmt.Errorf("no supplied session id")
	}

	natType := message.NAT
	if natType == "" {
		natType = "unknown"
	}

	return message.Sid, message.Type, natType, nil
}

type ProxyPollResponse struct {
	Status string
	Offer  string
	NAT    string
}

func EncodePollResponse(offer string, success bool, natType string) ([]byte, error) {
	if success {
		return json.Marshal(ProxyPollResponse{
			Status: "client match",
			Offer:  offer,
			NAT:    natType,
		})

	}
	return json.Marshal(ProxyPollResponse{
		Status: "no match",
	})
}

// Decodes a poll response from the broker and returns an offer and the client's NAT type
// If there is a client match, the returned offer string will be non-empty
func DecodePollResponse(data []byte) (string, string, error) {
	var message ProxyPollResponse

	err := json.Unmarshal(data, &message)
	if err != nil {
		return "", "", err
	}
	if message.Status == "" {
		return "", "", fmt.Errorf("received invalid data")
	}

	if message.Status == "client match" {
		if message.Offer == "" {
			return "", "", fmt.Errorf("no supplied offer")
		}
	} else {
		message.Offer = ""
	}

	natType := message.NAT
	if natType == "" {
		natType = "unknown"
	}

	return message.Offer, natType, nil
}

type ProxyAnswerRequest struct {
	Version string
	Sid     string
	Answer  string
}

func EncodeAnswerRequest(answer string, sid string) ([]byte, error) {
	return json.Marshal(ProxyAnswerRequest{
		Version: version,
		Sid:     sid,
		Answer:  answer,
	})
}

// Returns the sdp answer and proxy sid
func DecodeAnswerRequest(data []byte) (string, string, error) {
	var message ProxyAnswerRequest

	err := json.Unmarshal(data, &message)
	if err != nil {
		return "", "", err
	}

	majorVersion := strings.Split(message.Version, ".")[0]
	if majorVersion != "1" {
		return "", "", fmt.Errorf("using unknown version")
	}

	if message.Sid == "" || message.Answer == "" {
		return "", "", fmt.Errorf("no supplied sid or answer")
	}

	return message.Answer, message.Sid, nil
}

type ProxyAnswerResponse struct {
	Status string
}

func EncodeAnswerResponse(success bool) ([]byte, error) {
	if success {
		return json.Marshal(ProxyAnswerResponse{
			Status: "success",
		})

	}
	return json.Marshal(ProxyAnswerResponse{
		Status: "client gone",
	})
}

func DecodeAnswerResponse(data []byte) (bool, error) {
	var message ProxyAnswerResponse
	var success bool

	err := json.Unmarshal(data, &message)
	if err != nil {
		return success, err
	}
	if message.Status == "" {
		return success, fmt.Errorf("received invalid data")
	}

	if message.Status == "success" {
		success = true
	}

	return success, nil
}
