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

func CreateSnowflakeRequest(id string) []byte {
	request := &SnowflakeRequest{
		SnowflakeID: id,
	}
	jsonRequest, _ := json.Marshal(request)

	return jsonRequest
}
