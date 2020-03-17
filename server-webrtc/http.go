// An HTTP-based signaling channel for the WebRTC server. It imitates the
// broker as seen by clients, but it doesn't connect them to an
// intermediate WebRTC proxy, rather connects them directly to this WebRTC
// server. This code should be deleted when we have proxies in place.

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/pion/webrtc/v2"
)

type httpHandler struct {
	config *webrtc.Configuration
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`HTTP signaling channel

Send a POST request containing an SDP offer. The response will
contain an SDP answer.
`))
		if err != nil {
			log.Printf("GET request write failed with error: %v", err)
		}
		return
	case "POST":
		break
	default:
		http.Error(w, "Bad request.", http.StatusBadRequest)
		return
	}

	// POST handling begins here.
	body, err := ioutil.ReadAll(http.MaxBytesReader(w, req.Body, 100000))
	if err != nil {
		http.Error(w, "Bad request.", http.StatusBadRequest)
		return
	}
	offer := deserializeSessionDescription(string(body))
	if offer == nil {
		http.Error(w, "Bad request.", http.StatusBadRequest)
		return
	}

	pc, err := makePeerConnectionFromOffer(offer, h.config)
	if err != nil {
		http.Error(w, fmt.Sprintf("Cannot create offer: %s", err), http.StatusInternalServerError)
		return
	}

	log.Println("answering HTTP POST")

	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(serializeSessionDescription(pc.LocalDescription())))
	if err != nil {
		log.Printf("answering HTTP POST write failed with error %v", err)
	}

}

func receiveSignalsHTTP(addr string, config *webrtc.Configuration) error {
	http.Handle("/", &httpHandler{config})
	log.Printf("listening HTTP on %s", addr)
	return http.ListenAndServe(addr, nil)
}

func deserializeSessionDescription(msg string) *webrtc.SessionDescription {
	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(msg), &parsed)
	if nil != err {
		log.Println(err)
		return nil
	}
	if _, ok := parsed["type"]; !ok {
		log.Println("Cannot deserialize SessionDescription without type field.")
		return nil
	}
	if _, ok := parsed["sdp"]; !ok {
		log.Println("Cannot deserialize SessionDescription without sdp field.")
		return nil
	}

	var stype webrtc.SDPType
	switch parsed["type"].(string) {
	default:
		log.Println("Unknown SDP type")
		return nil
	case "offer":
		stype = webrtc.SDPTypeOffer
	case "pranswer":
		stype = webrtc.SDPTypePranswer
	case "answer":
		stype = webrtc.SDPTypeAnswer
	case "rollback":
		stype = webrtc.SDPTypeRollback
	}

	return &webrtc.SessionDescription{
		Type: stype,
		SDP:  parsed["sdp"].(string),
	}
}

func serializeSessionDescription(desc *webrtc.SessionDescription) string {
	bytes, err := json.Marshal(*desc)
	if nil != err {
		log.Println(err)
		return ""
	}
	return string(bytes)
}
