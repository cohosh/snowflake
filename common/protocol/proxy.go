//Package for a safer logging wrapper around the standard logging package

//import "git.torproject.org/pluggable-transports/snowflake.git/common/protocol"
package proto

import (
	"encoding/json"
	"fmt"
)

const version = "1.0"

// Version 1.0 specification:
//
// ProxyPollRequest:
// {
//   Sid: [generated session id of proxy]
//   Version: 1.0
// }

type ProxyPollRequest struct {
	Sid     string
	Version string
}

func EncodePollRequest(sid string) ([]byte, error) {
	return json.Marshal(ProxyPollRequest{
		Sid:     sid,
		Version: version,
	})
}

// Decodes a poll message from a snowflake proxy and returns the
// sid of the proxy on success and an error if it failed
func DecodePollRequest(data []byte) (string, error) {
	var message ProxyPollRequest

	err := json.Unmarshal(data, &message)
	if err != nil {
		return "", err
	}
	if message.Version != "1.0" {
		return "", fmt.Errorf("using unknown version")
	}

	// Version 1.0 requires an Sid
	if message.Sid == "" {
		return "", fmt.Errorf("no supplied session id")
	}

	return message.Sid, nil
}

type ProxyPollResponse struct {
	Status string
	Offer  string
}

func EncodePollResponse(offer string, success bool) ([]byte, error) {
	if success {
		return json.Marshal(ProxyPollResponse{
			Status: "client match",
			Offer:  offer,
		})

	}
	return json.Marshal(ProxyPollResponse{
		Status: "no match",
	})
}

// Decodes a poll response from the broker and returns an offer
// If there is a client match, the returned offer string will be non-empty
func DecodePollResponse(data []byte) (string, error) {
	var message ProxyPollResponse

	err := json.Unmarshal(data, &message)
	if err != nil {
		return "", err
	}
	if message.Status == "" {
		return "", fmt.Errorf("received invalid data")
	}

	if message.Status == "client match" {
		if message.Offer == "" {
			return "", fmt.Errorf("no supplied offer")
		}
	} else {
		message.Offer = ""
	}

	return message.Offer, nil
}

type ProxyAnswerRequest struct {
	Version string
	Sid     string
	Answer  string
}

func EncodeAnswerRequest(answer string, sid string) ([]byte, error) {
	return json.Marshal(ProxyAnswerRequest{
		Version: "1.0",
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
	if message.Version != "1.0" {
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
