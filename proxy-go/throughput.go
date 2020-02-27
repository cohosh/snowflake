package main

import (
	"encoding/json"
)

type SnowflakeRequest struct {
	SnowflakeID string `json:"snowflake_id"`
}

type SnowflakeOffer struct {
	Offer string `json:"offer"`
}

type SnowflakeAnswer struct {
	SnowflakeID string `json:"snowflake_id"`
	Answer      string `json:"answer"`
}

type SnowflakeResult struct {
	Throughput float64 `json:"throughput"`
	Latency    int     `json:"latency"`
	Error      string  `json:"error"`
}

func CreateSnowflakeRequest(id string) []byte {
	request := &SnowflakeRequest{
		SnowflakeID: id,
	}
	jsonRequest, _ := json.Marshal(request)

	return jsonRequest
}

func CreateSnowflakeAnswer(id string, answer string) []byte {
	request := &SnowflakeAnswer{
		SnowflakeID: id,
		Answer:      answer,
	}
	jsonRequest, _ := json.Marshal(request)

	return jsonRequest
}
