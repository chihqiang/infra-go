package handler

import (
	"encoding/json"
	"net/http"
)

type statusBody struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func writeStatusJSON(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(statusBody{Code: status, Msg: message})
}
