package helpers

import (
	"encoding/json"
	"log"
)

type APIResponse struct {
	StatCode int         `json:"stat_code"`
	StatMsg  string      `json:"stat_msg,omitempty"`
	ErrMsg   string      `json:"err_msg,omitempty"`
	Data     interface{} `json:"data,omitempty"`
}

func ApiResponse(data *APIResponse) []byte {
	res, err := json.Marshal(data)
	if err != nil {
		log.Printf("Formatting json response error: %v", err)
	}
	return res
}
